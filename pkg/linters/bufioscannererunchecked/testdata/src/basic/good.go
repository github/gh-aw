package basic

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func goodExample() error {
	file, _ := os.Open("test.txt")
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		println(scanner.Text())
	}
	// GOOD: scanner.Err() is checked
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func goodExampleSimpleCheck() {
	file, _ := os.Open("test.txt")
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		println(scanner.Text())
	}
	// GOOD: scanner.Err() is checked
	if scanner.Err() != nil {
		panic("scan error")
	}
}

func goodExampleWithMultipleStatements() error {
	file, _ := os.Open("test.txt")
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		println(scanner.Text())
	}
	// GOOD: scanner.Err() is checked after some other statement
	fmt.Println("finished scanning")
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func goodExampleWithReader() error {
	r := strings.NewReader("hello\nworld")
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		_ = scanner.Text()
	}
	// GOOD: scanner.Err() is checked
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func notAScanner() {
	// This should not be flagged (not using bufio.Scanner)
	m := make(map[string]string)
	for k := range m {
		println(k)
	}
}

func simpleForLoop() {
	// This should not be flagged (regular for loop, not scanner.Scan())
	for i := 0; i < 10; i++ {
		println(i)
	}
}

func differentScanner() {
	file, _ := os.Open("test.txt")
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		println(scanner.Text())
	}
	// GOOD: scanner.Err() is checked before different scanner
	if err := scanner.Err(); err != nil {
		panic("error")
	}

	// This should not be flagged because this is a different scanner
	anotherScanner := bufio.NewScanner(file)
	if anotherScanner.Err() != nil {
		panic("error")
	}
}

func goodExampleWithFunctionCall() error {
	file, _ := os.Open("test.txt")
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		processLine(scanner.Text())
	}
	// GOOD: scanner.Err() is checked
	return scanner.Err()
}

func processLine(s string) {
	println(s)
}

func goodScannerParameter(scanner *bufio.Scanner) error {
	for scanner.Scan() {
		println(scanner.Text())
	}
	return scanner.Err()
}

type checkedScannerHolder struct {
	scanner *bufio.Scanner
}

func goodSelectorScanner(holder checkedScannerHolder) error {
	for holder.scanner.Scan() {
		println(holder.scanner.Text())
	}
	return holder.scanner.Err()
}

func goodNestedBlock() error {
	file, _ := os.Open("test.txt")
	defer file.Close()

	if file != nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			println(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}
	return nil
}

func goodMultipleErrChecksSameStatement(scanner *bufio.Scanner, other *bufio.Scanner) error {
	for scanner.Scan() {
		println(scanner.Text())
	}
	if other.Err() != nil || scanner.Err() != nil {
		return scanner.Err()
	}
	return nil
}

func goodScanInLoopBody(scanner *bufio.Scanner) error {
	for {
		if !scanner.Scan() {
			break
		}
		println(scanner.Text())
	}
	return scanner.Err()
}

func goodClosureInsideLoop(scanner *bufio.Scanner) error {
	for i := 0; i < 1; i++ {
		func() {
			for scanner.Scan() {
				println(scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				panic(err)
			}
		}()
	}
	return nil
}
