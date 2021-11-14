package filtertag

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	deep_copy "github.com/mitchellh/copystructure"
)

type Logger struct {
	Output       io.Writer
	Filtertags   map[string]bool
	ExitFunc     func(int)
	OverflowFunc func()
}

type Entry struct {
	Fields   map[string]interface{}
	LoggerCh chan *LoggerChType
	ChDown   chan *LoggerChType

	prevEntryFiltertag string
	rawLine            []byte
}

type LoggerChType struct {
	Command       int
	Logger        *Logger
	CookedLogLine *CookedLogLine
	ChDown        chan *LoggerChType
}

type CookedLogLine struct {
	Filtertag string
	RawLine   []byte
}

const (
	Cmd_WriteLine int = iota
	Cmd_GetLogger
	Cmd_SetLogger
	Cmd_ExitFunc
)

func MakePrimordialEntryWithLogger(ctx context.Context) (entry *Entry) {
	var err error

	logger := &Logger{
		// these are defaults, you can change these by API
		Output: os.Stderr,
		Filtertags: map[string]bool{
			"INFO":                        true,
			"ERROR":                       true,
			"FATAL":                       true,
			"PANIC":                       true,
			"INPRODENV":                   true,
			"INVESTIGATETOMORROW":         true,
			"WAKEMEINTHEMIDDLEOFTHENIGHT": true,
			"EXITFUNC":                    true,
		},
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
			"filtertag":  "INPRODENV",
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
					if v, ok := logger.Filtertags[msg.CookedLogLine.Filtertag]; ok && v == true {
						_, err = logger.Output.Write(msg.CookedLogLine.RawLine)
						if err != nil {
							panic(fmt.Errorf("!!! filtertag.go:109 / *** at \"_, err = logger.Output.Write( msg.CookedLogLine.RawLine )\": %v", err))
						}
					}
				case Cmd_GetLogger:
					// we can't return the original, because a user may start touching it, and it'll race-condition-crash the program

					logger2, err := deep_copy.Copy(logger)
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

					logger2, _ := deep_copy.Copy(msg.Logger)
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

	entry2, err := deep_copy.Copy(entry)
	if err != nil {
		panic(fmt.Errorf("!!! filtertag.go:185 / *** at \"entry2, err := deep_copy.Copy( entry)\": %v", err))
	}

	return entry2.(*Entry)
}

type Writer struct {
	Entry *Entry
}

func (entry *Entry) Writer() (w io.Writer) {
	w = &Writer{
		Entry: entry,
	}
	return w
}
func (w *Writer) Write(p []byte) (n int, err error) {
	w.Entry.Log(string(p))
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
}

func (entry *Entry) WriterNestedJSON(key string) (w io.Writer) {
	w = &WriterNestedJSON{
		Entry:         entry,
		KeyNestedJSON: key,
	}
	return w
}

func (w *WriterNestedJSON) Write(p []byte) (n int, err error) {

	json := json.RawMessage(p)
	w.Entry.Fields[w.KeyNestedJSON] = &json

	w.Entry.Log("nested json in %v", w.KeyNestedJSON)

	w.Entry.Fields[w.KeyNestedJSON] = nil

	n = len(p)
	return
}

func (w *WriterNestedJSON) WriteStruct() {
	return
}

// Normal, base logging func
func (entry *Entry) Logft(
	filtertag string,
	formatstring string,
	args ...interface{},
) {
	var err error

	// save default filtertag
	if ft, ok := entry.Fields["filtertag"]; ok {
		entry.prevEntryFiltertag = ft.(string)
	} else {
		entry.prevEntryFiltertag = ""
	}

	entry.Fields["filtertag"] = strings.ToUpper(filtertag)
	entry.Fields["msg"] = fmt.Sprintf(formatstring, args...)

	// THIS MUST STAY HERE NO MATTER WHAT
	entry.Fields["timestamp"] = time.Now().Format("2006-01-02 15:04:05.000 MST")

	entry.rawLine, err = json.Marshal(entry.Fields)
	if err != nil {
		panic(fmt.Errorf("!!! filtertag.go:308 / *** at \"entry.rawLine, err = json.Marshal( entry.Fields)\": %v", err))
	}

	entry.rawLine = append(entry.rawLine, []byte("\n")...)

	msg := &LoggerChType{
		Command: Cmd_WriteLine,
		CookedLogLine: &CookedLogLine{
			Filtertag: filtertag,
			RawLine:   entry.rawLine,
		},
	}

	entry.LoggerCh <- msg

	// restore default filtertag, if it was set
	entry.Fields["filtertag"] = entry.prevEntryFiltertag

	entry.Fields["err"] = ""
	entry.Fields["msg"] = ""
}

// Normal, shorter, filtertag is inferred from the Entry
func (entry *Entry) Log(
	formatstring string,
	args ...interface{},
) {
	var filtertag string
	if ft, ok := entry.Fields["filtertag"]; ok {
		filtertag = ft.(string)
	} else {
		filtertag = "INPRODENV"
	}

	entry.Logft(filtertag, formatstring, args...)
}

// Log and exit the app
func (entry *Entry) ExitFunc(
	formatstring string,
	args ...interface{},
) {
	entry.Logft("EXITFUNC", formatstring, args...)
	entry.LoggerCh <- &LoggerChType{Command: Cmd_ExitFunc}
}

func (entry *Entry) InTestEnv(
	formatstring string,
	args ...interface{},
) {
	entry.Logft("INTESTENV", formatstring, args...)
}
func (entry *Entry) InProdEnv(
	formatstring string,
	args ...interface{},
) {
	entry.Logft("INPRODENV", formatstring, args...)
}
func (entry *Entry) InvestigateTomorrow(
	formatstring string,
	args ...interface{},
) {
	entry.Logft("INVESTIGATETOMORROW", formatstring, args...)
}
func (entry *Entry) WakeMeInTheMiddleOfTheNight(
	formatstring string,
	args ...interface{},
) {
	entry.Logft("WAKEMEINTHEMIDDLEOFTHENIGHT", formatstring, args...)
}

// A synonymical old-style funcs follow
type ClassicEntry struct {
	Entry *Entry
}

func (classic *ClassicEntry) Trace(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("TRACE", formatstring, args...)
}

func (classic *ClassicEntry) Debug(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("DEBUG", formatstring, args...)
}

func (classic *ClassicEntry) Info(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("INFO", formatstring, args...)
}

func (classic *ClassicEntry) Warning(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("WARNING", formatstring, args...)
}

func (classic *ClassicEntry) Error(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("ERROR", formatstring, args...)
}

func (classic *ClassicEntry) Emergency(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("EMERGENCY", formatstring, args...)
}

func (classic *ClassicEntry) Alert(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("ALERT", formatstring, args...)
}

func (classic *ClassicEntry) Critical(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("CRITICAL", formatstring, args...)
}

func (classic *ClassicEntry) Warn(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("WARN", formatstring, args...)
}

func (classic *ClassicEntry) Notice(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("NOTICE", formatstring, args...)
}

func (classic *ClassicEntry) Informational(
	formatstring string,
	args ...interface{},
) {
	classic.Entry.Logft("INFORMATIONAL", formatstring, args...)
}

func Hello() {
	fmt.Println("HELLO FILTERTAG")
}
