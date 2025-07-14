package types

const (
	NIL = iota
	LISTENER
	SESSION
	SCRIPT
	LOOT
)

type Profile struct {
	Prompt      string

	STATE int
}

type Command struct {
	Call   func([]string, *Profile) int // Callback entrypoint
	Usage  func([]string)               // help function callback
	Desc   string                       // hight level description.
	Prompt [][]string                   // Prompt help and auto-complete for subcommands
}