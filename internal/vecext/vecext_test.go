package vecext

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestSmoke(t *testing.T) {
	Auto()
	db, err := sql.Open("sqlite3", t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var ver, vec string
	if err := db.QueryRow("select sqlite_version(), vec_version()").Scan(&ver, &vec); err != nil {
		t.Fatal("vec_version:", err)
	}
	t.Log("sqlite", ver, "vec", vec)

	if _, err := db.Exec(`create virtual table v using vec0(id integer primary key, e float[3] distance_metric=cosine)`); err != nil {
		t.Fatal("create vec0:", err)
	}
	for id, v := range map[int][]float32{1: {1, 0, 0}, 2: {0.9, 0.1, 0}, 3: {0, 1, 0}} {
		b, _ := SerializeFloat32(v)
		if _, err := db.Exec(`insert into v(id,e) values (?,?)`, id, b); err != nil {
			t.Fatal("insert:", err)
		}
	}
	q, _ := SerializeFloat32([]float32{1, 0, 0})
	rows, err := db.Query(`select id, distance from v where e match ? and k = ? order by distance`, q, 3)
	if err != nil {
		t.Fatal("knn:", err)
	}
	defer rows.Close()
	var n int
	for rows.Next() {
		var id int
		var d float64
		if err := rows.Scan(&id, &d); err != nil {
			t.Fatal(err)
		}
		t.Log("hit", id, d)
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal("rows:", err)
	}
	if n != 3 {
		t.Fatalf("ожидалось 3 результата, получено %d", n)
	}
}
