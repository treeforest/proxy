package pb

import "net/http"

func NewHeaderWithHttpHeader(header http.Header) Header {
	h := Header{Header: make(map[string]Header_Value)}
	h.SetWithHttpHeader(header)
	return h
}

func (m *Header) ToHttpHeader() http.Header {
	header := http.Header{}
	for k, v := range m.Header {
		header[k] = v.Value
	}
	return header
}

func (m *Header) SetWithHttpHeader(header http.Header) {
	if header == nil {
		return
	}

	for k, vv := range header {
		m.Header[k] = Header_Value{Value: vv}
	}
}
