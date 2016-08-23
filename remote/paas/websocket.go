package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yinqiwen/gsnova/common/event"
	"github.com/yinqiwen/gsnova/remote"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

// handleWebsocket connection. Update to
func websocketInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		//log.WithField("err", err).Println("Upgrading to websockets")
		http.Error(w, "Error Upgrading to websockets", 400)
		return
	}
	ctx := remote.NewConnContext()
	writeEvents := func(evs []event.Event) error {
		if len(evs) > 0 {
			var buf bytes.Buffer
			for _, ev := range evs {
				if nil != ev {
					event.EncryptEvent(&buf, ev, ctx.IV)
				}
			}
			if buf.Len() > 0 {
				return ws.WriteMessage(websocket.BinaryMessage, buf.Bytes())
			}
			return nil
		}
		return nil
	}
	//log.Printf("###Recv websocket connection")
	buf := bytes.NewBuffer(nil)

	wsClosed := false
	var queue *remote.ConnEventQueue
	defer remote.ReleaseEventQueue(queue)
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			if err != io.EOF {
				log.Printf("Websoket read error:%v", err)
			}
			wsClosed = true
			break
		}
		switch mt {
		case websocket.BinaryMessage:
			if buf.Len() == 0 {
				buf = bytes.NewBuffer(data)
			} else {
				buf.Write(data)
			}
			ress, err := remote.HandleRequestBuffer(buf, ctx)
			if nil != err {
				log.Printf("[ERROR]connection %s:%d error:%v", ctx.User, ctx.ConnIndex, err)
				ws.Close()
				wsClosed = true
			} else {
				writeEvents(ress)
				if nil == queue && len(ctx.User) > 0 && ctx.ConnIndex >= 0 {
					queue = remote.GetEventQueue(ctx.ConnId, true)
					go func() {
						for !wsClosed {
							evs, err := queue.PeekMulti(2, 1*time.Millisecond)
							if ctx.Closing {
								evs = []event.Event{&event.ChannelCloseACKEvent{}}
							} else {
								if nil != err {
									continue
								}
							}
							err = writeEvents(evs)
							if ctx.Closing {
								return
							}
							if nil != err {
								log.Printf("Websoket write error:%v", err)
								return
							} else {
								queue.DiscardPeeks()
							}
						}
						remote.ReleaseEventQueue(queue)
					}()
				}
			}
		default:
			log.Printf("Invalid websocket message type")
			ws.Close()
		}
	}
	wsClosed = true
	log.Printf("Close websocket connection:%d", ctx.ConnIndex)
	//ws.WriteMessage(websocket.CloseMessage, []byte{})
}
