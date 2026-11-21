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

func unrelatedStat(path string) int64 {
	info, _ := Stat(path)
	return info.Size()
}

func Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
