package filtertag

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	deepCopy "github.com/mitchellh/copystructure"
)

type Logger struct {
	Output            io.Writer
	FiltertagsProRule string
	ExitFunc          func(int)
	OverflowFunc      func()
}

type Entry struct {
	Fields   map[string]interface{}
	LoggerCh chan *LoggerChType
	ChDown   chan *LoggerChType

	prevEntryFiltertag string
	rawLine            []byte
}

type LoggerChType struct {
	Command        int
	Logger         *Logger
	RawLine        []byte
	RuleASTPointer *filtertagpro.RuleAST
	ChDown         chan *LoggerChType
}

const (
	Cmd_WriteLine int = iota
	Cmd_GetLogger
	Cmd_SetLogger
	Cmd_ExitFunc
	Cmd_GetRuleASTPointer
)

func MakePrimordialEntryWithLogger(ctx context.Context) (entry *Entry) {
	var err error

	logger := &Logger{
		// these are defaults, you can change these by API
		Output: os.Stderr,
		FiltertagsProRule: `
			IF {
				anyof . {INFO ERROR FATAL PANIC INPRODENV INVESTIGATETOMORROW WAKEMEINTHEMIDDLEOFTHENIGHT EXITFUNC}
				// here we have always logger here, so "."
			} THEN {
				LOG
			}
			`,
	}

	logger.ExitFunc = func(i int) {
		os.Stderr.Sync()
		os.Exit(i)
	}

	logger.OverflowFunc = func() {
		os.Stderr.WriteString("FATAL ERROR AT FILTERTAG: main channel overflow, system failure.\n")
		os.Stderr.Sync()
		logger.ExitFunc(1)
	}

	ch_i1 := make(chan *LoggerChType, 502)
	host, err := os.Hostname()
	if err != nil {
		panic(fmt.Errorf("!!! filtertag.go:81 / *** at \"host, err	:= os.Hostname()\": %v", err))
	}
	executable, err := os.Executable()
	if err != nil {
		panic(fmt.Errorf("!!! filtertag.go:82 / *** at \"executable, err	:= os.Executable()\": %v", err))
	}

	entry = &Entry{
		Fields: map[string]interface{}{
			"timestamp":  "",
			"host":       host,
			"service":    executable,
			"subsystem":  "",
			"filtertag":  "INFO",
			"ctxpretext": "",
			"err":        "",
			"msg":        "",
		},
		LoggerCh: ch_i1,
	}

	go func() {
		var msg *LoggerChType
		for {
			select {
			case msg = <-ch_i1:
				if len(ch_i1) >= 500 {
					logger.OverflowFunc()
				}
				switch msg.Command {
				case Cmd_WriteLine:
					_, err = logger.Output.Write(msg.RawLine)
					if err != nil {
						panic(fmt.Errorf("!!! filtertag.go:109 / *** at \"_, err = logger.Output.Write( msg.CookedLogLine.RawLine )\": %v", err))
					}
				case Cmd_GetLogger:
					// we can't return the original, because a user may start touching it, and it'll race-condition-crash the program

					logger2, err := deepCopy.Copy(logger)
					if err != nil {
						continue
					}

					func() {
						defer func() {
							if errrec := recover(); errrec != nil {
								err = fmt.Errorf("try{} panicked at \"msg.Logger = logger2.(*Logger)\": %v", errrec)
							}
						}()
						err = nil
						msg.Logger = logger2.(*Logger)
					}()
					if err != nil {
						continue
					}

					msg.Logger.Output = logger.Output

					select {
					case msg.ChDown <- msg:
					default:
					}
				case Cmd_SetLogger:
					// we can't set the original, because a user may start touching it, and it'll race-condition-crash the program

					logger2, _ := deepCopy.Copy(msg.Logger)
					if err != nil {
						continue
					}

					func() {
						defer func() {
							if errrec := recover(); errrec != nil {
								err = fmt.Errorf("try{} panicked at \"logger = logger2.(*Logger)\": %v", errrec)
							}
						}()
						err = nil
						logger = logger2.(*Logger)
					}()

					logger.Output = msg.Logger.Output
				case Cmd_ExitFunc:
					logger.ExitFunc(1)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return entry
}

// DO NOT run Get/Set logger concurrently! Only one thread is allowed to run them
// (technically you can, and it will (kind of) work; but most certainly it's gonna be a bug due to race condition)
func (entry *Entry) GetLogger() (logger *Logger) {

	if entry.ChDown == nil {
		entry.ChDown = make(chan *LoggerChType, 1)
	}

	msg := &LoggerChType{Command: Cmd_GetLogger, Logger: logger, ChDown: entry.ChDown}

	entry.LoggerCh <- msg

	select {
	case <-time.After(2000 * time.Millisecond):
		panic(fmt.Errorf(", \"msg = <-entry.ChDown @!! 2000\""))
	case msg = <-entry.ChDown:
	}

	return msg.Logger
}

func (entry *Entry) SetLogger(logger *Logger) {

	msg := &LoggerChType{Command: Cmd_SetLogger, Logger: logger}

	entry.LoggerCh <- msg

	return
}

func (entry *Entry) Copy() *Entry {

	entry2, err := deepCopy.Copy(entry)
	if err != nil {
		panic(fmt.Errorf("!!! filtertag.go:185 / *** at \"entry2, err := deep_copy.Copy( entry)\": %v", err))
	}

	return entry2.(*Entry)
}

func (entry *Entry) GetRuleASTPointer(logger *Logger) {

	if entry.ChDown == nil {
		entry.ChDown = make(chan *LoggerChType, 1)
	}

	msg := &LoggerChType{Command: Cmd_GetRuleASTPointer, Logger: logger, ChDown: entry.ChDown}

	entry.LoggerCh <- msg

	select {
	case <-time.After(2000 * time.Millisecond):
		panic(fmt.Errorf(", \"msg = <-entry.ChDown @!! 2000\""))
	case msg = <-entry.ChDown:
	}

	//	return msg.Logger
}

type Writer struct {
	Entry      *Entry
	Filtertags []string
}

func (entry *Entry) Writer(
	filtertags []string,
) (w io.Writer) {
	w = &Writer{
		Entry:      entry,
		Filtertags: filtertags,
	}
	return w
}
func (w *Writer) Write(p []byte) (n int, err error) {
	w.Entry.Logft(w.Filtertags, string(p))
	n = len(p)
	return
}

// If you use filtertag.Writer(), the message end up logged as a text string in "msg" JSON key;
// that's not what we want here. Therefore, we will use a unique feature of filtertag,
// the WriterNestedJSON() function, which will nest the output as nested struct into the
// specified JSON key.
type WriterNestedJSON struct {
	Entry         *Entry
	KeyNestedJSON string
	Filtertags    []string
}

func (entry *Entry) WriterNestedJSON(
	filtertags []string,
	key string,
) (w io.Writer) {
	w = &WriterNestedJSON{
		Entry:         entry,
		KeyNestedJSON: key,
		Filtertags: filtertags,
	}
	return w
}

func (w *WriterNestedJSON) Write(p []byte) (n int, err error) {

	json := json.RawMessage(p)
	w.Entry.Fields[w.KeyNestedJSON] = &json

	w.Entry.Logft(w.Filtertags, "nested json in %v", w.KeyNestedJSON)

	w.Entry.Fields[w.KeyNestedJSON] = nil

	n = len(p)
	return
}

func (w *WriterNestedJSON) WriteStruct() {
	return
}

// The very base logging function
func (entry *Entry) Logft(
	filtertags []string,
	formatString string,
	args ...interface{},
) {
	var err error

	msg := &LoggerChType{
		Command: Cmd_WriteLine,
	}

	for i, _ := range filtertags {
		filtertags[i] = strings.ToUpper(filtertags[i])
	}
	// filtertags array must be allocated _new_ one on user-side, to avoid race conditions
	entry.Fields["filtertags"] = map[string][]string{
		"logger": filtertags,
	}
	entry.Fields["msg"] = fmt.Sprintf(formatString, args...)

	// THIS MUST STAY HERE NO MATTER WHAT
	entry.Fields["timestamp"] = time.Now().Format("2006-01-02 15:04:05.000 MST")

	msg.RawLine, err = json.Marshal(entry.Fields)
	if err != nil {
		panic(fmt.Errorf("!!! filtertag.go:308 / *** at \"entry.rawLine, err = json.Marshal( entry.Fields)\": %v", err))
	}
	msg.RawLine = append(msg.RawLine, []byte("\n")...)

	entry.LoggerCh <- msg

	entry.Fields["filtertag"] = nil
	entry.Fields["err"] = ""
	entry.Fields["msg"] = ""
}

// Log and exit the app
func (entry *Entry) ExitFunc(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"EXITFUNC"}, formatString, args...)
	entry.LoggerCh <- &LoggerChType{Command: Cmd_ExitFunc}
}

func (entry *Entry) InTestEnv(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"INTESTENV"}, formatString, args...)
}
func (entry *Entry) InProdEnv(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"INPRODENV"}, formatString, args...)
}
func (entry *Entry) InvestigateTomorrow(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"INVESTIGATETOMORROW"}, formatString, args...)
}
func (entry *Entry) WakeMeInTheMiddleOfTheNight(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"WAKEMEINTHEMIDDLEOFTHENIGHT"}, formatString, args...)
}

func (entry *Entry) Trace(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"TRACE", "L1"}, formatString, args...)
}

func (entry *Entry) Debug(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"DEBUG", "L2"}, formatString, args...)
}

func (entry *Entry) Info(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"INFO", "L3"}, formatString, args...)
}

func (entry *Entry) Warning(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"WARNING", "L4"}, formatString, args...)
}

func (entry *Entry) Error(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"ERROR", "L5"}, formatString, args...)
}

func (entry *Entry) Alert(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"ALERT", "L6"}, formatString, args...)
}

func (entry *Entry) Emergency(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"EMERGENCY", "L7"}, formatString, args...)
}

func (entry *Entry) Critical(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"CRITICAL", "L7"}, formatString, args...)
}

func (entry *Entry) Warn(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"WARN", "L4"}, formatString, args...)
}

func (entry *Entry) Notice(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"NOTICE", "L3"}, formatString, args...)
}

func (entry *Entry) Informational(
	formatString string,
	args ...interface{},
) {
	entry.Logft([]string{"INFORMATIONAL", "L3"}, formatString, args...)
}
