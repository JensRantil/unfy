package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/alecthomas/kong"
	"github.com/nleeper/goment"
)

// TODO: Not use CLI as a singleton. Instead of instantiate it in the main
// method to make all the functions testable.
var CLI struct {
	Milliseconds bool `help:"Search for UNIX timestamps in millisecond resolution. Without this, second resolution is expected. Currently, decimal points for UNIX timestamps isn't supported."`

	From time.Time `help:"The earliest UNIX timestamp we match. RFC3339." group:"Exact time span for UNIX timestamp matching. Defaults to --relative-interval if not defined. Flags:"`
	To   time.Time `help:"The latest UNIX timestamp we match. Uses RFC3339." group:"Exact time span for UNIX timestamp matching. Defaults to --relative-interval if not defined. Flags:"`

	RelTimeInterval time.Duration `name:"relative-interval" help:"The time interval +/- from current time for which UNIX timestamps are matched. Defaults to 10 years." default:"87600h"`

	Verbose bool `help:"Verbose logging to stderr. Useful for debugging." short:"v"`

	OutputMode string `name:"output-mode" help:"Whether the time should be absolute, relative, or both." enum:"absolute,relative,absolute+relative" default:"absolute"`

	PredefAbsoluteFormat string `name:"predefined-format" short:"p" help:"Predefined time format to replace UNIX timestamps with." enum:"RFC3339,RFC3339Nano,custom" default:"RFC3339"`
	Format               string `help:"Time format to replace UNIX timestamps with. Uses the same format as https://golang.org/pkg/time/#Parse with the exception that 'REL' gets replaced with a relative time." default:"2006-01-02T15:04:05Z07:00"`

	Unbuffered bool `help:"Don't buffer output. This will slow down the application."`

	Paths []*os.File `arg name:"path" help:"Paths to list." default:"-"`
}

var number *regexp.Regexp

func init() {
	number = regexp.MustCompile(fmt.Sprintf(`0*(?P<number>[1-9][0-9]{0,%d})`, len(strconv.FormatInt(math.MaxInt64, 10))))
	number.Longest()
}

func main() {
	kong.Parse(&CLI, kong.Description("A command line utility that will replace UNIX timestamps with human interpretable timestamps."))

	unixRange := newUnixRange(newTimeRange())
	timeConverter := newTimeConverter()
	formatter := newTimeFormatter()

	scanner := newScanner()
	output := newBufferedWriter(os.Stdout)
	defer output.Flush()

	splitter := &numberSplitter{}
	scanner.Split(splitter.Split)

	matcher := newMatcher(unixRange)
	for scanner.Scan() {
		data := scanner.Bytes()

		numberLoc := splitter.NumberLoc
		if numberLoc == nil {
			if _, err := output.Write(data); err != nil {
				fatalLn("Unable to write output:", err)
			}
			continue
		}
		unix, match := matcher.Match(data[numberLoc[2]:numberLoc[3]])
		if !match {
			if _, err := output.Write(data); err != nil {
				fatalLn("Unable to write output:", err)
			}
			continue
		}
		tstamp := timeConverter(unix)
		toPrint := formatter.Format(tstamp)
		if _, err := output.Write([]byte(toPrint)); err != nil {
			fatalLn("Unable to write output:", err)
		}
	}
	if err := scanner.Err(); err != nil {
		fatalLn("Invalid input:", err)
	}
}

func newBufferedWriter(out io.Writer) *bufio.Writer {
	if CLI.Unbuffered {
		return bufio.NewWriterSize(out, 0)
	}
	return bufio.NewWriter(out)
}

func newScanner() *bufio.Scanner {
	readers := make([]io.Reader, len(CLI.Paths))
	for i, p := range CLI.Paths {
		readers[i] = p
	}
	return bufio.NewScanner(bufio.NewReader(io.MultiReader(readers...)))
}

type timeRange struct {
	Lower time.Time
	Upper time.Time
}

func newTimeRange() timeRange {
	useAbsolute := !CLI.From.IsZero() || !CLI.To.IsZero()
	if useAbsolute {
		return timeRange{
			Lower: CLI.From,
			Upper: CLI.To,
		}
	}

	now := time.Now()
	return timeRange{
		Lower: now.Add(-CLI.RelTimeInterval),
		Upper: now.Add(CLI.RelTimeInterval),
	}
}

func newUnixRange(r timeRange) unixRange {
	if CLI.Milliseconds {
		return unixRange{
			Lower: r.Lower.UnixNano() / nanosPerMs,
			Upper: r.Upper.UnixNano() / nanosPerMs,
		}
	}
	return unixRange{
		Lower: r.Lower.Unix(),
		Upper: r.Upper.Unix(),
	}
}

func newTimeConverter() func(unix int64) time.Time {
	if CLI.Milliseconds {
		return millisecondConverter
	}
	return secondConverter
}

const nanosPerMs = int64(time.Millisecond / time.Nanosecond)

func millisecondConverter(unix int64) time.Time {
	seconds, nanos := unix/1000, nanosPerMs*(unix%1000)
	return time.Unix(seconds, nanos)
}

func secondConverter(unix int64) time.Time {
	return time.Unix(unix, 0)
}

func newTimeFormatter() timeFormatter {
	switch CLI.OutputMode {
	case "absolute":
		return newAbsoluteFormatter()
	case "relative":
		return relativeFormatter{}
	case "absolute+relative":
		return combinedFormatter{
			Base:        newAbsoluteFormatter(),
			Parenthesis: relativeFormatter{},
		}
	default:
		panic(fmt.Sprintf("unexpected mode: %s", CLI.OutputMode))
	}

}

type timeFormatter interface {
	Format(time.Time) string
}

func newAbsoluteFormatter() absoluteFormatter {
	return absoluteFormatter{timeFormat()}
}

func timeFormat() string {
	switch CLI.PredefAbsoluteFormat {
	case "RFC3339":
		return time.RFC3339
	case "RFC3339Nano":
		return time.RFC3339Nano
	case "custom":
		return CLI.Format
	default:
		panic(fmt.Sprintf("unexpected predefined format: %s", CLI.OutputMode))
	}
}

type absoluteFormatter struct {
	Layout string
}

func (g absoluteFormatter) Format(t time.Time) string {
	return t.Format(g.Layout)
}

type relativeFormatter struct {
}

func (g relativeFormatter) Format(t time.Time) string {
	// i'm not too fond of the api of goment.new(interface{}). would avoiding
	// using reflection here and instead be type-safe and not have to ignore
	// error. perhaps we should look for an alternative library.
	m, _ := goment.New(t)
	return m.FromNow()
}

type combinedFormatter struct {
	Base        timeFormatter
	Parenthesis timeFormatter
}

func (g combinedFormatter) Format(t time.Time) string {
	return fmt.Sprintf("%s (%s)", g.Base.Format(t), g.Parenthesis.Format(t))
}

func fatalLn(a ...interface{}) {
	fmt.Println(a...)
	os.Exit(1)
}

type numberSplitter struct {
	// NumberLoc contains the location the number in the returned data.
	NumberLoc    []int
	nextLoc      []int
	nextNeedMore bool
}

// Split implements bufio.SplitFunc. It splits into byte slices that either
// match the number regexp, or not. If it matches a number, n.NumberLoc will
// contain the exact location of the number in the previously returned token.
func (n *numberSplitter) Split(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if n.nextNeedMore {
		// Last time we saw we didn't have enough data for a full UNIX
		// timestamp. Avoiding a regular expression scan here and
		// simply ask for more data immediatelt.
		n.nextNeedMore = false
		return 0, nil, nil
	}
	if n.nextLoc != nil {
		// We have data from previous regexp scan. Avoid an unnecessary scan.
		loc := n.nextLoc
		n.NumberLoc = loc
		n.nextLoc = nil
		return loc[1] - loc[0], data[loc[0]:loc[1]], nil
	}

	loc := number.FindSubmatchIndex(data)
	if loc == nil {
		if atEOF {
			return len(data), data, bufio.ErrFinalToken
		}
		return len(data), data, nil
	}
	if loc[0] > 0 {
		// We found integers, but we first need to return some non-integer data.
		if loc[1] == len(data) {
			// We need to read more data.
			if atEOF {
				// ...but we can't. Return all data, but don't set n.NumberLoc since we didn't find any numbers.
				return len(data), data, bufio.ErrFinalToken
			}
			n.nextNeedMore = true
			return loc[0], data[0:loc[0]], nil
		}

		// We have enough data.
		toReturn := data[0:loc[0]]

		toSubtract := loc[0]
		loc[0] = 0
		loc[1] -= toSubtract
		loc[2] -= toSubtract
		loc[3] -= toSubtract
		n.nextLoc = loc

		return len(toReturn), toReturn, nil
	}

	n.NumberLoc = loc
	return loc[1] - loc[0], data[loc[0]:loc[1]], nil
}

type unixRange struct {
	Lower int64 // inclusive
	Upper int64 // inclusive
}

func (u unixRange) LowerString() string { return strconv.FormatInt(u.Lower, 10) }
func (u unixRange) UpperString() string { return strconv.FormatInt(u.Upper, 10) }
func (u unixRange) Contains(i int64) bool {
	return i >= u.Lower && i <= u.Upper
}

type matcher struct {
	uRange unixRange
	maxLen int
	minLen int
	prefix []byte
}

func newMatcher(r unixRange) matcher {
	lowerString, upperString := r.LowerString(), r.UpperString()
	return matcher{
		uRange: r,
		maxLen: max(len(lowerString), len(upperString)),
		minLen: min(len(lowerString), len(upperString)),
		prefix: buildPrefix([]byte(lowerString), []byte(upperString)),
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Match converts a series of bytes to UNIX timestamp. It quickly disregards
// byte slices that it's sure can't be a UNIX timestamp.
func (u matcher) Match(b []byte) (conversion int64, match bool) {
	if length := len(b); length < u.minLen || length > u.maxLen {
		return 0, false
	}
	if !bytes.HasPrefix(b, u.prefix) {
		return 0, false
	}
	conversion, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, false
	}
	return conversion, u.uRange.Contains(conversion)
}

func buildPrefix(a, b []byte) []byte {
	if len(a) < len(b) {
		return buildPrefixOrdered(a, b)
	}
	return buildPrefixOrdered(b, a)
}

func buildPrefixOrdered(shorter, longer []byte) []byte {
	res := make([]byte, 0, len(shorter))
	for i, b := range shorter {
		if longer[i] != b {
			return res
		}
		res = append(res, b)
	}
	return res
}
