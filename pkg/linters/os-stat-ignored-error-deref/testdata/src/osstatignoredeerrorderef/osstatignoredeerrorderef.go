package osstatignoredeerrorderef

import "os"

func ignoredStatError(path string) int64 {
	info, _ := os.Stat(path)
	return info.Size() // want "result of os.Stat is dereferenced"
}

func ignoredLstatError(path string) string {
	info, _ := os.Lstat(path)
	return info.Name() // want "result of os.Lstat is dereferenced"
}

func ignoredStatDeclaration(path string) int64 {
	var info, _ = os.Stat(path)
	return info.Size() // want "result of os.Stat is dereferenced"
}

func ignoredLstatDeclaration(path string) string {
	var info, _ = os.Lstat(path)
	return info.Name() // want "result of os.Lstat is dereferenced"
}

func ignoredAssignmentError(path string) int64 {
	var info os.FileInfo
	info, _ = os.Stat(path)
	return info.Size() // want "result of os.Stat is dereferenced"
}

func ignoredErrorInIfInitializer(path string) bool {
	if info, _ := os.Stat(path); info.Size() > 0 { // want "result of os.Stat is dereferenced"
		return true
	}
	return false
}

func checkedForNonNil(path string) int64 {
	info, _ := os.Stat(path)
	if info != nil {
		return info.Size()
	}
	return 0
}

func checkedForNil(path string) int64 {
	info, _ := os.Stat(path)
	if info == nil {
		return 0
	}
	return info.Size()
}

func guardedInConditionalSibling(path string, enabled bool) int64 {
	info, _ := os.Stat(path)
	if enabled {
		if info == nil {
			return 0
		}
	}
	return info.Size() // want "result of os.Stat is dereferenced"
}

func reassignedAfterNilGuard(path string) int64 {
	info, _ := os.Stat(path)
	if info == nil {
		return 0
	}
	info, _ = os.Stat(path)
	return info.Size() // want "result of os.Stat is dereferenced"
}

func reassignedWithRetainedError(path string) int64 {
	info, _ := os.Stat(path)
	var err error
	info, err = os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func guardedByShortCircuitAnd(path string) bool {
	info, _ := os.Stat(path)
	return info != nil && info.IsDir()
}

func guardedByShortCircuitOr(path string) bool {
	info, _ := os.Stat(path)
	return info == nil || info.IsDir()
}

func guardedIfShortCircuitAnd(path string) int64 {
	info, _ := os.Stat(path)
	if info != nil && info.IsDir() {
		return info.Size()
	}
	return 0
}

func guardedIfShortCircuitOr(path string) int64 {
	info, _ := os.Stat(path)
	if info == nil || !info.IsDir() {
		return 0
	}
	return info.Size()
}

func shadowedPanicDoesNotTerminate(path string) int64 {
	info, _ := os.Stat(path)
	panic := func() {}
	if info == nil {
		panic()
	}
	return info.Size() // want "result of os.Stat is dereferenced"
}

func builtinPanicTerminates(path string) int64 {
	info, _ := os.Stat(path)
	if info == nil {
		panic("nil file info")
	}
	return info.Size()
}

func checkedInIfInitializer(path string) int64 {
	if info, _ := os.Lstat(path); info != nil {
		return info.Size()
	}
	return 0
}

func retainedError(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func expressionlessSwitch(path string) int64 {
	info, _ := os.Stat(path)
	switch {
	case info == nil:
		return 0
	default:
		return info.Size() // want "result of os.Stat is dereferenced"
	}
}

func unrelatedStat(path string) int64 {
	info, _ := Stat(path)
	return info.Size()
}

func Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
