Unfy
====
`unfy` is a command line utility that automagically identifies and translated
UNIX timestamps (since epoch) to human readable timestamps.

Example
-------
Basic example:
```bash
$ echo "Timestamp: 1613336683" | unfy
Timestamp: 2021-02-14T22:04:43+01:00
```
Parsing UNIX timestamps in millisecond resolution:
```bash
$ echo "Timestamp: 1613336683123" | unfy --milliseconds
Timestamp: 2021-02-14T22:04:43+01:00
$ echo "Timestamp: 1613336683123" | unfy --milliseconds --predefined-format RFC3339Nano
Timestamp: 2021-02-14T22:04:43.123+01:00
```
Outputting with relative time:
```bash
$ echo "Timestamp: 1613336683" | unfy --output-mode relative
Timestamp: 2 days ago
$ echo "Timestamp: 1613336683" | go run main.go --output-mode absolute+relative
Timestamp: 2021-02-14T22:04:43+01:00 (2 days ago)
```

The name?
---------
`unfy` is short for un-UNIX-fy.
