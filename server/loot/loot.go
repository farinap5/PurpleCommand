package loot

import (
	"os"
	"purpcmd/server/db"

	"github.com/cheynewallace/tabby"
	"github.com/google/uuid"
)

func New(s, n string, c []byte) *Loot {
	l := new(Loot)
	l.FileName = n
	l.Content = c
	l.Session = s
	l.UUID = uuid.New().String()
	return l
}

func (l *Loot)SaveData() error {
	file, err := os.OpenFile("loot/"+l.UUID, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	defer file.Close()
	_,err = file.Write(l.Content)
	if err != nil {
		return err
	}

	return db.DBLootInsert(l.UUID, l.Session, l.FileName)
}

func List() {
	t := tabby.New()
	c := 1
	t.AddHeader("N", "UUID", "SESSION", "FILENAME")

	l, _ := db.DBLootList()
	for i := range l {
		t.AddLine(c, l[i][0], l[i][1], l[i][2])
		c += 1
	}
	print("\n")
	t.Print()
	print("\n")
}