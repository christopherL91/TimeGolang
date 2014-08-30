package main

import (
	"encoding/json"
	"github.com/christopherL91/TimeGolang/gin"
	"github.com/clbanning/mxj"
	"github.com/gorilla/websocket"
	mgo "gopkg.in/mgo.v2"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"
)

var (
	mgo_session *mgo.Session
)

func init() {
	var err error
	mgo_session, err = mgo.Dial("127.0.0.1")
	if err != nil {
		panic(err)
	}
}

type Server struct {
	Connections map[*websocket.Conn]interface{}
	broadcastCh chan map[string]interface{}
	removeCh    chan *websocket.Conn
	mutex       *sync.Mutex
}

func (s *Server) addConnection(conn *websocket.Conn) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.Connections[conn] = nil
}

func (s *Server) removeConnection(conn *websocket.Conn) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.Connections, conn)
	log.Println("closed connection")
}

func (s *Server) sendMessage(data map[string]interface{}, conn *websocket.Conn) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := conn.WriteJSON(data); err != nil {
		s.removeCh <- conn
	}
}

func (s *Server) broadcast() {
	for {
		select {
		case message := <-s.broadcastCh:
			for connection := range s.Connections {
				s.sendMessage(message, connection)
			}
		case conn := <-s.removeCh:
			s.removeConnection(conn)
		}
	}
}

func main() {
	r := gin.Default()
	server := &Server{
		Connections: make(map[*websocket.Conn]interface{}, 0),
		broadcastCh: make(chan map[string]interface{}, 100),
		removeCh:    make(chan *websocket.Conn, 100),
		mutex:       new(sync.Mutex),
	}

	go server.broadcast()

	public := r.Group("/api")

	public.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"hello": "hello world"})
	})

	public.GET("/login/kth", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "https://login.kth.se/login?service=http://localhost:3000/api/login/callback/kth")
	})

	public.GET("/ws", func(c *gin.Context) {
		conn, err := websocket.Upgrade(c.Writer, c.Request, nil, 1024, 1024)
		if err != nil {
			c.Fail(500, err)
			return
		}
		server.addConnection(conn)
		time.Sleep(1000)
		//server.broadcastCh <- data
		log.Println("FOO")
	})
	/*
		{
			user: "id",
			action: "start"
		}
	*/
	public.POST("/sessions/:session/logs", func(c *gin.Context) {
		data := map[string]interface{}{
			"greeting": "Hello, World!",
		}
		server.broadcastCh <- data
	})

	public.GET("/login/callback/kth", func(c *gin.Context) {
		mgo_conn := mgo_session.Copy()
		defer mgo_conn.Close()

		ticket := c.Request.URL.Query().Get("ticket")
		client := new(http.Client)
		// kth_user := new(KTH_User)

		res, err := client.Get("https://login.kth.se/serviceValidate?ticket=" + ticket + "&service=http://localhost:3000/api/login/callback/kth")
		if err != nil {
			c.Fail(500, err)
			return
		}
		data, err := ioutil.ReadAll(res.Body)
		if err != nil {
			c.Fail(500, err)
			return
		}
		doc, err := mxj.NewMapXml(data, false)
		if err != nil {
			c.Fail(500, err)
			return
		}
		serv := doc["serviceResponse"].(map[string]interface{})
		result := serv["authenticationSuccess"].(map[string]interface{})
		userid := result["user"].(string)

		res, err = client.Get("https://www.kth.se/social/api/profile/1.1/" + userid)
		if err != nil {
			c.Fail(500, err)
			return
		}

		data, err = ioutil.ReadAll(res.Body)
		if err != nil {
			c.Fail(500, err)
			return
		}

		var profile map[string]interface{}
		err = json.Unmarshal(data, &profile)
		if err != nil {
			c.Fail(500, err)
			return
		}
		userData := map[string]interface{}{
			"kthid":      userid,
			"firstname":  profile["givenName"],
			"lastname":   profile["familyName"],
			"profilepic": profile["image"],
		}
		id := updateLoginInfo(mgo_conn, userData)
		payload := make(map[string]interface{})
		payload["name"] = profile["givenName"]
		payload["id"] = id
		payload["exp"] = time.Now().Add(time.Hour * 24).Unix()
		token, err := generateToken([]byte("I<3Unicorns"), &payload)
		if err != nil {
			c.Fail(500, err)
			return
		}
		c.JSON(200, gin.H{"token": token})
	})

	r.Run("localhost:3000")
}
