package lua

import (
	"purpcmd/server/log"

	"github.com/cheynewallace/tabby"
)

func ScriptList() {
	if len(ScriptMAP) == 0 {
		log.PrintAlert("no script")
	}

	t := tabby.New()
	c := 1
	t.AddHeader("N", "PATH", "LOADED")
	for k, v := range ScriptMAP {
		t.AddLine(c, k, v.Running)
		c += 1
	}
	print("\n")
	t.Print()
	print("\n")
}