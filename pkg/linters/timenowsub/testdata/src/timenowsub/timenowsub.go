package timenowsub

import (
	faketime "faketime/time"
	"time"
)

func bad(t time.Time) {
	_ = time.Now().Sub(t) // want `time\.Now\(\)\.Sub\(t\) can be simplified to time\.Since\(t\)`
}

func badAssign(start time.Time) time.Duration {
	return time.Now().Sub(start) // want `time\.Now\(\)\.Sub\(start\) can be simplified to time\.Since\(start\)`
}

type state struct {
	start time.Time
}

func badSelector(s state) time.Duration {
	return time.Now().Sub(s.start) // want `time\.Now\(\)\.Sub\(s\.start\) can be simplified to time\.Since\(s\.start\)`
}

func badIndex(starts []time.Time, i int) time.Duration {
	return time.Now().Sub(starts[i]) // want `time\.Now\(\)\.Sub\(starts\[i\]\) can be simplified to time\.Since\(starts\[i\]\)`
}

func badWithComments(start time.Time) time.Duration {
	return time.Now().Sub(start /* measured from init */) // want `time\.Now\(\)\.Sub\(start\) can be simplified to time\.Since\(start\)`
}

func good(t time.Time) {
	_ = time.Since(t)
}

func goodOtherSub(a, b time.Time) {
	_ = a.Sub(b)
}

func goodCallExprArg() {
	_ = time.Now().Sub(loadStart())
}

func goodSelectorCallArg() time.Duration {
	return time.Now().Sub(loadState().start)
}

func goodIndexCallArg(starts []time.Time) time.Duration {
	return time.Now().Sub(starts[nextIndex()])
}

func goodOtherTimePackage(t faketime.Time) {
	_ = faketime.Now().Sub(t)
}

func loadStart() time.Time {
	return time.Now()
}

func loadState() state {
	return state{start: time.Now()}
}

func nextIndex() int {
	return 0
}
