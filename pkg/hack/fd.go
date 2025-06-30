package hack

import (
	"net"
	"reflect"
	"unsafe"
)

func unexportedField(v reflect.Value, name string) reflect.Value {
	f := v.FieldByName(name)
	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
}

// realFD pulls the underlying fd.sysfd (or pfd.Sysfd) out of a *net.UnixConn.
func GetFdFromUnixConn(conn *net.UnixConn) (int, error) {
	// 1) reflect.Value of the conn struct (it’s a wrapper around an embedded conn)
	v := reflect.ValueOf(conn).Elem()

	// 2) grab the embedded conn field
	connField := unexportedField(v, "conn")

	// 3) inside that is an unexported *netFD called "fd"
	fdPtr := unexportedField(connField, "fd").Elem()

	// 4) depending on Go version it might be “sysfd” or wrapped in a poll.FD
	// Try sysfd first:
	if sysfdField := fdPtr.FieldByName("sysfd"); sysfdField.IsValid() {
		return int(sysfdField.Int()), nil
	}

	// Otherwise fall back to pfd.Sysfd
	pfd := unexportedField(fdPtr, "pfd")
	sysfd := unexportedField(pfd, "Sysfd").Int()
	return int(sysfd), nil
}
