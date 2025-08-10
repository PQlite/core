package p2p

import (
	"bytes"
	"encoding/json"
	"log"
	"time"

	"github.com/PQlite/core/chain"
)

// Читання вхідних повідомлень
func (n *Node) handleBroadcastMessages() {
	for {
		msg, err := n.topic.sub.Next(n.ctx)
		if err != nil {
			log.Println("помилка при отриманні повідомлення: ", err)
		}

		var message Message
		err = json.Unmarshal(msg.Data, &message)
		if err != nil {
			log.Println("помилка розпаковки повідомлення: ", err)
			continue
		}

		if msg.ReceivedFrom == n.host.ID() && message.Type != MsgBlockProposal { // HACK: якщо цього не буде, то воно не зможе запустити створення ще оного блоку
			log.Println("повідомлення від себе")
			continue
		}

		if !message.verify() {
			log.Println("підпис повідомлення not valid")
			continue
		}

		switch message.Type {
		case MsgNewTransaction:
			var tx chain.Transaction
			err = json.Unmarshal(message.Data, &tx)
			if err != nil {
				continue
			}

			err = n.mempool.Add(&tx)
			if err != nil {
				log.Println("отрмана транзакція не була додана до mempool через", err)
			}
		case MsgBlockProposal:
			var block chain.Block
			err = json.Unmarshal(message.Data, &block)
			if err != nil {
				log.Println("помилка розпаковки blockProposal")
				continue
			}
			log.Printf("отримано новий блок: %d", block.Height)

			// NOTE: я ще не впевнений в MsgVote, тому що, якщо я перевірив блок, і він правельний, то це означає, що усі за нього проголосують
			// TODO: додати перевірку автора ( щоб pubkey збігався з тим, хто повинен був робити блок ). І нагороду, яку він собі назначив
			// TODO: видалити транзакції з mempool, якщо вони вже є в блоці
			isBlockValid := block.Verify()
			isBlocksTXsValids := block.VerifyTransactions()

			lastLocalBlock, err := n.bs.GetLastBlock()
			if err != nil {
				panic(err)
			}

			if isBlockValid && isBlocksTXsValids && lastLocalBlock.Height < block.Height {
				go n.bs.SaveBlock(&block)

				for _, tx := range block.Transactions {
					if bytes.Equal(tx.To, []byte(STAKE)) {
						validator := chain.Validator{
							Address: tx.PubKey, // NOTE: досі не вирішив, чи я використовую hash або pub
							Amount:  tx.Amount,
						}

						n.bs.AddValidator(&validator)
					}
				}

				val, err := n.chooseValidator()
				if err != nil {
					log.Println("помилка вибору наступного валідатора, ", err)
					continue
				}

				// я це і є настпуний валідатор!
				if bytes.Equal(val.Address, n.keys.Pub) {
					newBlock := n.createNewBlock()

					newBlockBytes, err := json.Marshal(newBlock)
					if err != nil {
						panic(err)
					}

					blockProposalMsg := Message{
						Type:      MsgBlockProposal,
						Timestamp: time.Now().UnixMilli(),
						Data:      newBlockBytes,
						Pub:       n.keys.Pub,
					}

					err = blockProposalMsg.sign(n.keys.Priv)
					if err != nil {
						panic(err)
					}

					go n.topic.broadcast(&blockProposalMsg, n.ctx)

					n.bs.SaveBlock(&newBlock) // NOTE: треба буде переробити, якщо я хочу робити Vote
				}
				continue
			}
			log.Println("блок не є валідним")
		}

		latency := time.Now().UnixMilli() - message.Timestamp
		log.Println("oтримано за ", latency, "ms")
	}
}
