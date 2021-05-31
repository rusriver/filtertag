# filtertag

The structured logging source/emitter library for Go.

The API is partially inspired by sirupsen/logrus, though the concepts are different, and un-orthodox at all.

The main difference from classic logging approach, is that here is no concept of "level", instead we operate with the "filter tag". The Filter Tag is arbitrary string, which denotes some specific "layer", or part of code, and you can arbitrarily use any number of these tags. There's no hierarchy of them. Instead, the
user myst specify a specific list of filter tags to be included in the output, or filtered/included at the visualization app.

This library still lacks the proper documentation, but it was __used in industrial production as early as 2019__ at molti.tech and AVTPROM, and proved to be solid good.

It emits only JSON.

__Interesting__: this library is 100% thread-safe, and has zero locks/mutexes. I.e. it is written in the best spirit of Go language. The concept is summarized in the document Effective Go (a must-read for any Go programmer):

   _Do not communicate by sharing memory; instead, share memory by communicating._

