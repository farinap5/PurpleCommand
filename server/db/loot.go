package db

func DBLootInsert(Uuid, Session, FileName string) error {
	/*if DBLootExist(Name) {
		return errors.New("listener exists")
	}*/

	insertQuery := `
	INSERT INTO Loot (Uuid, Session, FileName) VALUES (?,?,?);
	`
	_, err := DBMS.DBConn.Exec(insertQuery, Uuid, Session, FileName)

	return err
}


func DBLoot(uuid string) (string, string, string, error) {
	/*if DBLootExist(Name) {
		return errors.New("listener exists")
	}*/

	Query := `
	SELECT Uuid, Session, FileName FROM Loot WHERE Uuid LIKE '%' || ? || '%';
	`
	row := DBMS.DBConn.QueryRow(Query, uuid)

	var d1,d2,d3 string
	err := row.Scan(&d1,&d2,&d3)

	return d1, d2, d3, err
}

func DBLootList() ([][3]string, error) {
    query := `
    SELECT Uuid, Session, FileName FROM Loot;
    `
    rows, err := DBMS.DBConn.Query(query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var lootList [][3]string
    for rows.Next() {
        var uuid, session, fileName string
        if err := rows.Scan(&uuid, &session, &fileName); err != nil {
            return nil, err
        }
        lootList = append(lootList, [3]string{uuid, session, fileName})
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return lootList, nil
}