package types

import (
)


//ExtraNonce2 is the nonce modified by the miner
type ExtraNonce2 struct {
	Value uint64
	Size  uint
}

//Bytes is a bigendian representation of the extranonce2
func (en *ExtraNonce2) Bytes() (b []byte) {
	b = make([]byte, en.Size, en.Size)
	for i := uint(0); i < en.Size; i++ {
		b[(en.Size-1)-i] = byte(en.Value >> (i * 8))
	}
	return
}

//Increment increases the nonce with 1, an error is returned if the resulting is value is bigger than possible given the size
func (en *ExtraNonce2) Increment() (err error) {
	en.Value++
	//TODO: check if does not overflow compared to the allowed size
	return
}

// StratumRequest contains stratum request messages received over TCP
// request : A remote method is invoked by sending a request to the remote stratum service.
type StratumRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params []interface{}   `json:"params"`
}

// StratumResponse contains stratum response messages sent over TCP
// notification is an inline struct to easily decode messages in a response/notification
// using a json marshaller
type StratumResponse struct {
	ID     uint64          `json:"id"`
	Result interface{}     `json:"result"`
	Error  []interface{}   `json:"error"`
	StratumNotification    `json:",inline"`
}

// notification is a special kind of Request, it has no ID and is sent from the server
// to the client
type StratumNotification struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}
