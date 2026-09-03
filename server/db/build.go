package db

import (
	"database/sql"
	"errors"
	"time"

	"purpcmd/pkg/teamapi"
)

func DBBuildSave(build teamapi.Build) error {
	_, err := DBMS.DBConn.Exec(
		`INSERT INTO BuildJobs (ID, Profile, Builder, Status, ArtifactName, Error, CreatedAt, CompletedAt)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(ID) DO UPDATE SET Builder=excluded.Builder, Status=excluded.Status, ArtifactName=excluded.ArtifactName,
		 Error=excluded.Error, CompletedAt=excluded.CompletedAt;`,
		build.ID, build.Profile, build.Builder, build.Status, build.ArtifactName, build.Error,
		formatDBTime(build.CreatedAt), nullableDBTime(build.CompletedAt),
	)
	return err
}

func DBBuildDelete(id string) error {
	result, err := DBMS.DBConn.Exec(`DELETE FROM BuildJobs WHERE ID = ?;`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("build not found")
	}
	return nil
}

func DBBuildList() ([]teamapi.Build, error) {
	rows, err := DBMS.DBConn.Query(
		`SELECT ID, Profile, Builder, Status, ArtifactName, Error, CreatedAt, CompletedAt
		 FROM BuildJobs ORDER BY CreatedAt DESC;`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []teamapi.Build
	for rows.Next() {
		var build teamapi.Build
		var created string
		var completed sql.NullString
		if err := rows.Scan(&build.ID, &build.Profile, &build.Builder, &build.Status, &build.ArtifactName,
			&build.Error, &created, &completed); err != nil {
			return nil, err
		}
		build.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		if completed.Valid {
			build.CompletedAt, err = time.Parse(time.RFC3339Nano, completed.String)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, build)
	}
	return result, rows.Err()
}

func formatDBTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableDBTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatDBTime(value)
}
