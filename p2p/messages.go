package p2p

type MessageType string

const (
	// Мережеві повідомлення
	MsgPing     MessageType = "ping"
	MsgPong     MessageType = "pong"
	MsgPeerList MessageType = "peerList"

	// Транзакції
	MsgNewTransaction MessageType = "newTransaction"

	// Блоки
	MsgNewBlock     MessageType = "newBlock"
	MsgRequestBlock MessageType = "requestBlock"
	MsgResponeBlock MessageType = "responeBlock"

	// PoS
	MsgBlockProposal MessageType = "blockProposal"
	MsgVote          MessageType = "vote"
	MsgCommit        MessageType = "commit"

	// Валідатори
	MsgValidatorSet MessageType = "validatorSet"
)

type Message struct {
	Text      string `json:"text"`
	Timestrmp int64  `json"timestamp"`
}

type Message1 struct {
	Type      MessageType `json:"type"` // Тип повідомлення
	From      string      `json:"from"` // ID відправника
	To        string      `json:"to"`   // ID отримувача, але якщо порожнє - для всіх
	Timestamp int64       `json:"timestamp"`
	Data      []byte      `json:"data"`      // дані через json.Marshal
	Signature []byte      `json:"signature"` // підпис відправника
}
