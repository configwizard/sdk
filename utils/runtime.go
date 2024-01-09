package utils

import (
	"fmt"
	"runtime"
)

func PrintDebugInfo() {
	// Print the number of goroutines
	numGoroutines := runtime.NumGoroutine()
	fmt.Println("Number of goroutines: ", numGoroutines)

	// Print the stack trace
	buf := make([]byte, 1024)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			fmt.Println("Stack trace:", string(buf[:n]))
			break
		}
		buf = make([]byte, 2*len(buf))
	}
}
