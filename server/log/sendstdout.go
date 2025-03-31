package log

import "fmt"

// Types of pretty printing
func PrintSuccs(a ...any) {
	fmt.Printf("[\u001B[1;32mOK\u001B[0;0m]- %s\n", fmt.Sprint(a...))
}

func PrintErr(a ...any) {
	fmt.Printf("[\u001B[1;31m!\u001B[0;0m]- %s\n", fmt.Sprint(a...))
}

func PrintAlert(a ...any) {
	fmt.Printf("[\u001B[1;31m!\u001B[0;0m]- %s\n", fmt.Sprint(a...))
}

func PrintInfo(a ...any) {
	fmt.Printf("[\u001B[1;34mi\u001B[0;0m]- %s\n", fmt.Sprint(a...))
}

func PrintLn(a ...any) {
	fmt.Print(fmt.Sprint(a...), "\n")
}