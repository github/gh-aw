package basic

import (
	"bufio"
	"os"
	"strings"
)

func badExample() {
	file, _ := os.Open("test.txt")
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
		println(scanner.Text())
	}
	// BUG: No scanner.Err() check
}

func badExampleInIf() error {
	file, _ := os.Open("test.txt")
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
		println(scanner.Text())
	}
	// BUG: No scanner.Err() check
	return nil
}

func badExampleWithReader() error {
	r := strings.NewReader("hello\nworld")
	scanner := bufio.NewScanner(r)
	for scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
		_ = scanner.Text()
	}
	return nil
}

func badMultipleScanners() {
	file1, _ := os.Open("test1.txt")
	defer file1.Close()
	file2, _ := os.Open("test2.txt")
	defer file2.Close()

	scanner1 := bufio.NewScanner(file1)
	for scanner1.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
		println(scanner1.Text())
	}

	scanner2 := bufio.NewScanner(file2)
	for scanner2.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
		println(scanner2.Text())
	}
}

func badNolintIgnored() {
	file, _ := os.Open("test.txt")
	defer file.Close()

	scanner := bufio.NewScanner(file)
	//nolint:bufioscannererunchecked
	for scanner.Scan() {
		println(scanner.Text())
	}
	// With nolint, this should not be flagged
}

func badNestedBlock() {
	file, _ := os.Open("test.txt")
	defer file.Close()

	if file != nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
			println(scanner.Text())
		}
	}
}

func badVarScanner() {
	file, _ := os.Open("test.txt")
	defer file.Close()

	var scanner = bufio.NewScanner(file)
	for scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
		println(scanner.Text())
	}
}

func badReassignedScanner() {
	file, _ := os.Open("test.txt")
	defer file.Close()

	var scanner *bufio.Scanner
	scanner = bufio.NewScanner(file)
	for scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
		println(scanner.Text())
	}
}

func badScannerParameter(scanner *bufio.Scanner) {
	for scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
		println(scanner.Text())
	}
}

type scannerHolder struct {
	scanner *bufio.Scanner
}

func badSelectorScanner(holder scannerHolder) {
	for holder.scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
		println(holder.scanner.Text())
	}
}

func badInsideOuterLoop(file *os.File) {
	for i := 0; i < 1; i++ {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
			println(scanner.Text())
		}
	}
}

func badScanInLoopBody(scanner *bufio.Scanner) {
	for { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
		if !scanner.Scan() {
			break
		}
		println(scanner.Text())
	}
}

func badSwitchCase(scanner *bufio.Scanner, mode string) {
	switch mode {
	case "scan":
		for scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
			println(scanner.Text())
		}
	}
}

func badSelectCase(scanner *bufio.Scanner, ch <-chan struct{}) {
	select {
	case <-ch:
		for scanner.Scan() { // want "Scanner loop does not check Err\\(\\) after completion; read errors may be silently dropped"
			println(scanner.Text())
		}
	default:
	}
}
