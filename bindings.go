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
	return C.td_json_client_create()
}

func tgExecute(client unsafe.Pointer, query genericMap) {
	b, err := json.Marshal(query)
	if err != nil {
		panic(err)
	}
	s := C.CString(string(b))
	defer C.free(unsafe.Pointer(s))
	C.td_json_client_execute(client, s)
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
