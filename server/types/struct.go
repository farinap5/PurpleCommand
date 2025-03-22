package types

type Profile struct {
	Prompt      string

	Listener 	bool
	Session		bool
}

type Command struct {
	Call   func([]string, *Profile) int // Callback entrypoint
	Usage  func([]string)               // help function callback
	Desc   string                       // hight level description.
	Prompt [][]string                   // Prompt help and auto-complete for subcommands
}