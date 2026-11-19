package sidecar

import "encoding/binary"

// SNIFromClientHello returns the server name from a TLS ClientHello record, or
// "" when there is none or the record is incomplete or malformed. It never
// returns a partial name: a length field that runs past the bytes received
// yields "", and the caller falls back to the original destination.
func SNIFromClientHello(buf []byte) string {
	if len(buf) < 5 || buf[0] != 22 {
		return ""
	}
	recLen := int(binary.BigEndian.Uint16(buf[3:5]))
	if 5+recLen > len(buf) {
		return "" // record not fully received
	}
	hs := buf[5 : 5+recLen]
	if len(hs) < 42 || hs[0] != 1 {
		return ""
	}
	i := 38 // handshake type (1), length (3), version (2), random (32)
	if i >= len(hs) {
		return ""
	}
	i += 1 + int(hs[i]) // session id
	if i+2 > len(hs) {
		return ""
	}
	i += 2 + int(binary.BigEndian.Uint16(hs[i:i+2])) // cipher suites
	if i+1 > len(hs) {
		return ""
	}
	i += 1 + int(hs[i]) // compression methods
	if i+2 > len(hs) {
		return ""
	}
	extTotal := int(binary.BigEndian.Uint16(hs[i : i+2]))
	i += 2
	if i+extTotal > len(hs) {
		return "" // extensions run past the record
	}
	end := i + extTotal
	for i+4 <= end {
		etype := binary.BigEndian.Uint16(hs[i : i+2])
		elen := int(binary.BigEndian.Uint16(hs[i+2 : i+4]))
		i += 4
		if i+elen > end {
			return "" // this extension is cut short
		}
		data := hs[i : i+elen]
		i += elen
		if etype != 0 {
			continue
		}
		// server_name: list length (2), name type (1), name length (2), name.
		if len(data) < 5 {
			return ""
		}
		nlen := int(binary.BigEndian.Uint16(data[3:5]))
		if 5+nlen > len(data) {
			return ""
		}
		if nlen > 0 {
			return string(data[5 : 5+nlen])
		}
	}
	return ""
}
