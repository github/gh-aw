package resourcetracker

type resource struct{}

func acquire() *resource { return &resource{} }

func (r *resource) Release() error { return nil }

func ManualRelease() {
	r := acquire() // want `resource should be released with defer`
	r.Release()
}

func DeferredRelease() {
	r := acquire()
	defer r.Release()
}

func AcquiredAndLeaked() {
	r := acquire()
	_ = r
}

func WrapperClosureDeferredRelease() {
	r := acquire()
	defer func() {
		r.Release()
	}()
}

func ReleaseResultAssigned() {
	r := acquire() // want `resource should be released with defer`
	err := r.Release()
	_ = err
}

func ConditionalAcquisition(ok bool) {
	if ok {
		r := acquire() // want `resource should be released with defer`
		r.Release()
	}
}

func EarlyReturn() *resource {
	r := acquire()
	return r
}

func ClosureBodyNotTracked() {
	r := acquire()
	defer r.Release()

	fn := func() {
		// Closure bodies are independent execution contexts; no diagnostic expected.
		inner := acquire()
		inner.Release()
	}
	fn()
}

func ReassignReportsPreviousViolation() {
	r := acquire() // want `resource should be released with defer`
	r.Release()

	r = acquire()
	defer r.Release()
}

func NoLintSuppresses() {
	r := acquire() //nolint:resourcetrackertest
	r.Release()
}

func Shadowing() {
	r := acquire()
	defer r.Release()
	{
		r := acquire() // want `resource should be released with defer`
		r.Release()
	}

}

func SiblingCleanup(deferred bool) {
	r := acquire() // want `resource should be released with defer`
	if deferred {
		defer r.Release()
	} else {
		r.Release()
	}
}

func DifferentBranchAcquisitions(deferred bool) {
	var r *resource
	if deferred {
		r = acquire()
		defer r.Release()
	} else {
		r = acquire() // want `resource should be released with defer`
		r.Release()
	}
}

func BothBranchAcquisitionsManual(first bool) {
	var r *resource
	if first {
		r = acquire() // want `resource should be released with defer`
	} else {
		r = acquire() // want `resource should be released with defer`
	}
	r.Release()
}
