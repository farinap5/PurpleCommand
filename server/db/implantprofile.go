package db

import (
	"database/sql"
	"errors"
)

// ImplantProfile mirrors implantbuilder.Profile for DB storage.
type ImplantProfile struct {
	Name      string
	LHOST     string
	OS        string
	ARCH      string
	URI       string
	UA        string
	Output    string
	Template  string
	PublicKey string
}

func DBImplantProfileExists(name string) bool {
	var n string
	err := DBMS.DBConn.QueryRow("SELECT Name FROM ImplantProfiles WHERE Name = ?;", name).Scan(&n)
	return err == nil
}

func DBImplantProfileInsert(p ImplantProfile) error {
	if DBImplantProfileExists(p.Name) {
		return errors.New("profile already exists")
	}
	_, err := DBMS.DBConn.Exec(
		`INSERT INTO ImplantProfiles (Name, LHOST, OS, ARCH, URI, UA, Output, Template, PublicKey)
		 VALUES (?,?,?,?,?,?,?,?,?);`,
		p.Name, p.LHOST, p.OS, p.ARCH, p.URI, p.UA, p.Output, p.Template, p.PublicKey,
	)
	return err
}

func DBImplantProfileUpdate(p ImplantProfile) error {
	if !DBImplantProfileExists(p.Name) {
		return errors.New("profile not found")
	}
	_, err := DBMS.DBConn.Exec(
		`UPDATE ImplantProfiles
		 SET LHOST=?, OS=?, ARCH=?, URI=?, UA=?, Output=?, Template=?, PublicKey=?
		 WHERE Name=?;`,
		p.LHOST, p.OS, p.ARCH, p.URI, p.UA, p.Output, p.Template, p.PublicKey, p.Name,
	)
	return err
}

func DBImplantProfileDelete(name string) error {
	if !DBImplantProfileExists(name) {
		return errors.New("profile not found")
	}
	_, err := DBMS.DBConn.Exec(`DELETE FROM ImplantProfiles WHERE Name = ?;`, name)
	return err
}

func DBImplantProfileGetAll() ([]ImplantProfile, error) {
	var profiles []ImplantProfile
	rows, err := DBMS.DBConn.Query(
		`SELECT Name, LHOST, OS, ARCH, URI, UA, Output, Template, PublicKey FROM ImplantProfiles;`,
	)
	if err != nil {
		return nil, err
	}
	defer func(r *sql.Rows) { _ = r.Close() }(rows)

	for rows.Next() {
		var p ImplantProfile
		if err := rows.Scan(&p.Name, &p.LHOST, &p.OS, &p.ARCH, &p.URI, &p.UA, &p.Output, &p.Template, &p.PublicKey); err != nil {
			continue
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}
