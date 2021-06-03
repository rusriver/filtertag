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
	Output	io.Writer
	Filtertags	map[string]bool
	ExitFunc	func(int)
}

type Entry struct {
	Fields	map[string]interface{}
	LoggerCh	chan *LoggerChType
	ChDown	chan *LoggerChType
	
	prev_entry_filtertag	string
	rawline	[]byte
}

type LoggerChType struct {
	Command	int
	Logger	*Logger
	CookedLogLine	*CookedLogLine
	ChDown	chan *LoggerChType
}

type CookedLogLine struct {
	Filtertag	string
	RawLine	[]byte
}

const (
	Cmd_WriteLine int = iota
	Cmd_GetLogger
	Cmd_SetLogger
	Cmd_FatalExit
)

func MakePrimordialEntryWithLogger(ctx context.Context) (entry *Entry) {
	var err error
	
	logger := &Logger{
		// these are defaults, you can change these by API
		Output:	os.Stderr,
		Filtertags:	map[string]bool{
				"info": true,
				"error": true,
				"fatal": true,
				"panic": true,
			},
		ExitFunc:	os.Exit,
	}
	
	ch_i1	:= make(chan *LoggerChType, 500)
	host, err	:= os.Hostname(); if err != nil { panic(fmt.Errorf("!!! filtertag.go:66 / *** at \"host, err	:= os.Hostname()\": %v", err)) }
	executable, err	:= os.Executable(); if err != nil { panic(fmt.Errorf("!!! filtertag.go:67 / *** at \"executable, err	:= os.Executable()\": %v", err)) }
	
	entry = &Entry{
		Fields:	map[string]interface{}{
				"timestamp":	"",
				"host":	host,
				"service":	executable,
				"subsystem":	"",
				"filtertag":	"info",
				"ctxpretext":	"",
				"err":	"",
				"msg":	"",
			},
		LoggerCh:	ch_i1,
	}
	
	go func(){
		var msg *LoggerChType
		for { select {
		case msg = <-ch_i1:
			switch msg.Command {
			case Cmd_WriteLine:
				if v, ok := logger.Filtertags[ msg.CookedLogLine.Filtertag ]; ok && v == true {
					_, err = logger.Output.Write( msg.CookedLogLine.RawLine ); if err != nil { panic(fmt.Errorf("!!! filtertag.go:91 / *** at \"_, err = logger.Output.Write( msg.CookedLogLine.RawLine )\": %v", err)) }
				}
			case Cmd_GetLogger:
				// we can't return the original, because a user may start touching it, and it'll race-condition-crash the program
				
				logger2, err := deep_copy.Copy(logger)
				if err != nil { continue; }
				
				func(){ defer func() { if errrec := recover(); errrec != nil { err = fmt.Errorf("try{} panicked at \"msg.Logger = logger2.(*Logger)\": %v", errrec) }}(); err = nil; msg.Logger = logger2.(*Logger) }()
				if err != nil { continue; }
				
				msg.Logger.Output = logger.Output
				
				select { case msg.ChDown <- msg: default: }
			case Cmd_SetLogger:
				// we can't set the original, because a user may start touching it, and it'll race-condition-crash the program
				
				logger2, _ := deep_copy.Copy( msg.Logger)
				if err != nil { continue; }
				
				func(){ defer func() { if errrec := recover(); errrec != nil { err = fmt.Errorf("try{} panicked at \"logger = logger2.(*Logger)\": %v", errrec) }}(); err = nil; logger = logger2.(*Logger) }()
				
				logger.Output = msg.Logger.Output
			case Cmd_FatalExit:
				logger.ExitFunc(1)
			}
		case <-ctx.Done():
			return
		}}
	}()
	
	return entry
}

// DO NOT run Get/Set logger concurrently! Only one thread is allowed to run them
func (entry *Entry) GetLogger() (logger *Logger) {

	if entry.ChDown == nil {
		entry.ChDown = make(chan *LoggerChType, 1)
	}

	msg := &LoggerChType{ Command: Cmd_GetLogger, Logger: logger, ChDown: entry.ChDown }

	entry.LoggerCh <- msg
	
	select { case <-time.After(2000 * time.Millisecond): panic(fmt.Errorf(", \"msg = <-entry.ChDown @!! 200\"")); case msg = <-entry.ChDown: }

	return msg.Logger
}

func (entry *Entry) SetLogger(logger *Logger) {

	msg := &LoggerChType{ Command: Cmd_SetLogger, Logger: logger }

	entry.LoggerCh <- msg

	return
}


func (entry *Entry) Copy() (*Entry) {
	
	entry2, err := deep_copy.Copy( entry); if err != nil { panic(fmt.Errorf("!!! filtertag.go:166 / *** at \"entry2, err := deep_copy.Copy( entry)\": %v", err)) }

	return entry2.(*Entry)
}


type Writer struct{
	Entry	*Entry
}

func (w *Writer) Write(p []byte) (n int, err error) {
	w.Entry.Log( string(p) )
	n = len(p)
	return
}

func (entry *Entry) Writer() (w io.Writer) {
	w = &Writer{
		Entry: entry,
	}
	return w
}

// If you use filtertag.Writer(), the message end up logged as a text string in "msg" JSON key;
// that's not what we want here. Therefore, we will use a unique feature of filtertag,
// the WriterNestedJSON() function, which will nest the output as nested struct into
// specified JSON key.
type WriterNestedJSON struct{
	Entry	*Entry
	KeyNestedJSON:	string
}

func (w *WriterNestedJSON) Write(p []byte) (n int, err error) {

	w.Entry.Fields[ w.KeyNestedJSON ] = &json.RawMessage(p)

	w.Entry.Log("nested json in %v", w.KeyNestedJSON)
	
	w.Entry.Fields[ w.KeyNestedJSON ] = nil
	
	n = len(p)
	return
}

func (entry *Entry) WriterNestedJSON( key string ) (w io.Writer) {
	w = &WriterNestedJSON{
		Entry:	entry,
		KeyNestedJSON:	key,
	}
	return w
}

// Normal, base logging func
func (entry *Entry) Logft(
	filtertag	string,
	formatstring	string,
	args	...interface{},
) {
	var err error
	
	if ft, ok := entry.Fields["filtertag"]; ok {
		entry.prev_entry_filtertag = ft.(string)
	} else {
		entry.prev_entry_filtertag = ""
	}
	
	entry.Fields["filtertag"]	= strings.ToLower( filtertag )
	entry.Fields["msg"]	= fmt.Sprintf( formatstring, args...)
	entry.Fields["timestamp"]	= time.Now().Format("2006-01-02 15:04:05.000 MST")
	
	entry.rawline, err = json.Marshal( entry.Fields); if err != nil { panic(fmt.Errorf("!!! filtertag.go:246 / *** at \"entry.rawline, err = json.Marshal( entry.Fields)\": %v", err)) }
	
	entry.rawline = append(entry.rawline, []byte("\n")...)
	
	msg := &LoggerChType{
		Command:	Cmd_WriteLine,
		CookedLogLine:	&CookedLogLine{
				Filtertag:	filtertag,
				RawLine:	entry.rawline,
			},
	}

	entry.LoggerCh <- msg
	
	entry.Fields["filtertag"]	= entry.prev_entry_filtertag
	entry.Fields["err"]	= ""
	entry.Fields["msg"]	= ""
}

// Normal, shorter, filtertag is inferred from the Entry
func (entry *Entry) Log(
	formatstring	string,
	args	...interface{},
) {
	var filtertag string
	if ft, ok := entry.Fields["filtertag"]; ok {
		filtertag = ft.(string)
	} else {
		filtertag = "info"
	}

	entry.Logft( filtertag, formatstring, args...)
}


// Filtertag="fatal", and as a special case it commands to exit the program (via ExitFunc / os.Exit())
func (entry *Entry) Fatal(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "fatal", formatstring, args...)
	entry.LoggerCh <- &LoggerChType{ Command: Cmd_FatalExit }
}

// Filtertag="panic", and as a special case it panics
func (entry *Entry) Panic(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "panic", formatstring, args...)
	err := fmt.Errorf( formatstring, args...)
	panic(err)
}

// A synonymical funcs follow
func (entry *Entry) Trace(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "trace", formatstring, args...)
}

func (entry *Entry) Debug(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "debug", formatstring, args...)
}

func (entry *Entry) Info(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "info", formatstring, args...)
}

func (entry *Entry) Warning(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "warning", formatstring, args...)
}

func (entry *Entry) Error(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "error", formatstring, args...)
}

func (entry *Entry) Emergency(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "emergency", formatstring, args...)
}

func (entry *Entry) Alert(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "alert", formatstring, args...)
}

func (entry *Entry) Critical(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "critical", formatstring, args...)
}

func (entry *Entry) Warn(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "warn", formatstring, args...)
}

func (entry *Entry) Notice(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "notice", formatstring, args...)
}

func (entry *Entry) Informational(
	formatstring	string,
	args	...interface{},
) {
	entry.Logft( "informational", formatstring, args...)
}


func Hello() {
	fmt.Println("HELLO FILTERTAG")
}

