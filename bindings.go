package main

// #cgo LDFLAGS: -ltdjson
// #include <stdlib.h>
// #include <td/telegram/td_log.h>
// #include <td/telegram/td_json_client.h>
import "C"
import (
	"encoding/json"
	"unsafe"
)

type genericMap map[string]interface{}

func tgClient() unsafe.Pointer {
	C.td_set_log_verbosity_level(2) // warnings, debug warnings
	return C.td_json_client_create()
}

func tgSend(client unsafe.Pointer, query genericMap) {
	b, err := json.Marshal(query)
	if err != nil {
		panic(err)
	}
	s := C.CString(string(b))
	defer C.free(unsafe.Pointer(s))
	C.td_json_client_send(client, s)
}

func tgReceive(client unsafe.Pointer) string {
	return C.GoString(C.td_json_client_receive(client, 1.0))
}
