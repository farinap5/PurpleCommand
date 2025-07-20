package loot

import (
	"io"
	"os"
	"purpcmd/server/db"
	"purpcmd/server/log"

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
		t.AddLine(c, l[i][0][24:], l[i][1], l[i][2])
		c += 1
	}
	print("\n")
	t.Print()
	print("\n")
}

func Export(uuid, path string) error {
	name,fuuid, err := db.DBLootGetByUUID(uuid)
	if err != nil {
		return err
	}

	src, err := os.Open("loot/" + fuuid)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	if err != nil {
		return err
	}

	log.PrintSuccs("file ", fuuid, " ", name, " saved to ", path)
	return nil
}