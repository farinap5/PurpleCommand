package types

type Profile struct {

}

type Command struct {
	Call   func([]string, *Profile) int // Callback entrypoint
	Usage  func([]string)               // help function callback
	Desc   string                       // hight level description.
	Prompt [][]string                   // Prompt help and auto-complete for subcommands
}

type Listener struct {
	Host 	string
	Port 	string

	Proto 	string
	Persistent bool
}