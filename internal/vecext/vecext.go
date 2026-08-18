// Package vecext подключает расширение sqlite-vec к mattn/go-sqlite3 (CGO-драйвер).
//
// Пакет vendored: официальные биндинги github.com/asg017/sqlite-vec-go-bindings/cgo
// компилируются с флагом -DSQLITE_CORE, из-за чего sqlite-vec.h делает
// #include "sqlite3.h". Такого заголовка нет — mattn/go-sqlite3 хранит амальгамацию
// SQLite под именем sqlite3-binding.h, а системный libsqlite3-dev в проекте не
// используется. Поэтому исходники sqlite-vec скопированы сюда, а амальгамация
// mattn/go-sqlite3 подложена рядом под именем sqlite3.h, чтобы cgo нашёл её
// локально через -I${SRCDIR}.
package vecext

// #cgo CFLAGS: -DSQLITE_CORE -I${SRCDIR}
// #cgo linux LDFLAGS: -lm
// #include "sqlite-vec.h"
import "C"

import (
	"bytes"
	"encoding/binary"
)

// Auto регистрирует расширение sqlite-vec для всех новых соединений SQLite3
// в процессе.
func Auto() {
	C.sqlite3_auto_extension((*[0]byte)(C.sqlite3_vec_init))
}

// SerializeFloat32 сериализует вектор float32 в BLOB, понятный sqlite-vec.
func SerializeFloat32(vector []float32) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, vector); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
