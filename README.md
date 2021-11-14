# filtertag

The structured logging source/emitter library for Go.

The API is partially inspired by sirupsen/logrus, though the concepts are different, and un-orthodox at all.

The main difference from classic logging approach, is that here is no concept of "level", instead we operate with the "filter tag". The Filter Tag is arbitrary string, which denotes some specific "layer", or part of code, and you can arbitrarily use any number of these tags. There's no hierarchy of them. Instead, the
user myst specify a specific list of filter tags to be included in the output, or filtered/included at the visualization app.

This library still lacks the proper documentation, but it was __used in industrial production as early as 2019__ at molti.tech and AVTPROM, and proved to be solid good.

It emits JSON only.

__Interesting__: this library is 100% thread-safe, and has zero locks/mutexes. I.e. it is written in the best spirit of Go language. The concept is summarized in the document Effective Go (a must-read for any Go programmer):

   _Do not communicate by sharing memory; instead, share memory by communicating._


# Version history

This package DOES NOT use Go's version conventions any more, sorry for that, it's intentional decision.
Breaking changes are inroduced right onto a master branch, and if you want your old code to work and work
as expected, just depend on older git commits.

### 20210603-140715 {e7ec1210844471022e314bf62d2c5250c260bdf2}

This is version 1, battle-tested, the very first one published on the open Internet.

### 2021-11-14 on

Log levels (filtertags) refactoring according to the idea of Daniel Lebrero (https://labs.ig.com/logging-level-wrong-abstraction).

Also, there was this point of view https://www.reddit.com/r/programming/comments/66ftqf/comment/dgjhyt4/?utm_source=share&utm_medium=web2x&context=3
Quote:

IME there are three basic types of logging:

- human: the message should be seen by human operators
- developer: the message should be seen by a developer maintaining the project
- administrator: the message should be seen by the person administering the system

The whole idea of 7 or 8 log levels that are only vaguely distinguishable is completely over-engineered. Add to that the horrible syslog(3) API and Byzantine rules around what shows up in the log by default and what doesn't, and you've got everyone just shrugging and using LOG_NOTICE because that's the one that shows up in the log without being an error or warning of some kind.

END OF QUOTE

I don't quite agree, but it looks tempting (I even started to implement something like this, then quit).
__Let's clarify how it's supposed to work in real life:__

- There are testing/dev environments, where all logs are non-critical by definition. Period.
- There are prod environments, where logs are collected, monitored, filtered, and some reaction must happen:
    - In prod, __all logs first go to the administrator/devops__, then he/she may choose to alert a developer,
        even in the middle of the night.
    - Filtering may happen in several places, and have different effect:
        - Filtering at the source - this is what this library does, in particular;
        - Filtering/re-routing at the transport, when some logs leads to alerts in the middle of the night,
            some lead to to-be-investigated list, etc.
    - Therefore, we don't need human/admin/dev tags, just because on the prod everything goes whom
        it's intended or who is responsible by default. And then may be proxied to other parties.
- __Routing/filtering logs at transport/storage/analysis stages may work based on any data inside the log lines;
    but at the source, in this lib, we are specifically interested in answering the very simple question:
    should this particular log line be logged, or should it be dropped? (and "dropped" means nobody nowhere
    will be able to see it). This means:__
    - InTestEnv is enabled in testing/dev environment, and typically is disabled in prod;
    - InProdEnv is always enabled; synonym for classic "info";
    - InvestigateTomorrow and WakeMeInTheMiddleOfTheNight are also always enabled, and serve for filtering
        at the action-taking stage.... basically these aren't levels at all.

The updated working set of logging functions is as follows, with according filtertags:

- Logft(), same as previously
- Log(), same as previously
- ExitFunc(), log and exit the app, filtertag="EXITFUNC"
- InTestEnv(), filtertag equals the function name in uppercase
- InProdEnv(), filtertag equals the function name in uppercase
- InvestigateTomorrow(), filtertag equals the function name in uppercase
- WakeMeInTheMiddleOfTheNight(), filtertag equals the function name in uppercase

__All filtertags are now uppercase.__

The filtertag defaults are now:

	"INFO": true,
	"ERROR": true,
	"FATAL": true,
	"PANIC": true,
	"INPRODENV": true,
	"INVESTIGATETOMORROW": true,
	"WAKEMEINTHEMIDDLEOFTHENIGHT": true,
	"EXITFUNC": true,

Old functions and filtertags were preserved, as the tribute-to-history gesture, in the
ClassicEntry type, which are: Trace, Debug, Info, Warning, Error, Emergency, Alert, Critical,
Warn, Notice, Informational.

ExitFunc and panic you now have to call yourself explicitly, no more log methods that call them
implicitly: Panic() is deprecated, you have to call built-in panic(err) explicitly after logging
a reason of (that means, you may panic non-critically as well); Fatal() is also deprecated (renamed),
now you call the ExitFunc() instead, which will call the ExitFunc of logger after logging a reason.

Added OverflowFunc, which triggers when the main logger channel is (near) full; by default,
the function prints a message and calls ExitFunc, exiting the app. No more silent blocking
or performance degradation (e.g. when docker-compose logger drivers sucks reading stderr).

Default ExitFunc now also calls the os.Stderr.Sync() before exiting.

~Most heavy-weight operations (fmt.Sprintf() and json.Marshal() moved from user-side
Logft() to the logger-bound goroutine, thus offloading user goroutines of this work.
(This isn't necessarily good, because it also means more work aggregated in the single
logger goroutine; however, I decided to rebalance work this way, considering sporadic
nature of logging, and the fact that we have a buffer on the channel.)~ BAD IDEA, REJECTED.


# Plans to do next

- Write a documentation
- Draw diagrams to better understand the architecture
- Examples of use:
    - Basic example
    - Capture stderr/stdout
    - Get logs to NSQ (and then to Loki)
    - Basic example with Config
    - Advanced:
        - ctxpretext
        - sub-entries
        - embeded JSON
    - how to use ClassicEntry
    - using Writer, to attach this logger where io.Writer expected (e.g. Gin or Echo web-server)
    - using WriterNestedJSON, Write() and WriteStruct() (auto-marshaler), and also how to set the filtertag
- Implement:
    - Stdin/stdout intercept
    - logger output to io.Writer _and_/or chan *Entry
    - NSQ IO Writer
    - Remove hardcoded main chan limits to the Config (left default the 500)
    - func (w *WriterNestedJSON) WriteStruct()
- Integrate with terr package (idiom how to set those tags inside some Field as an array);
- Switch default output serialization format from JSON to Serk, ditch JSON support altogether. (Q: how about Loki
    integration? A: convert enroute in NSQ)
- Introduce a more lightweight and high-speed alternative to Logft(), which will use template with the pre-Marchaled data,
    so that you can avoid fmt.Sprintf and json.Marshal, and just send to a chan a [][]byte array, where chunks
    of prepared template are alternate with []byte arguments. (you call json.Marshal beforehand, then split by
    some injected tokens, and get this [][]byte, with even elements being part of JSON, and odd elements being
    slots to fill with user-specified []byte). Let's call it LogFastforward(). __Measure the difference.__



