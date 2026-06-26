package protocol

// Ack is a bidirectional receipt (Envelope.Type == TypeAck). RefID points at
// the id of the message being acknowledged.
type Ack struct {
	RefID string `json:"ref_id"`
}
