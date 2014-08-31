package main

import (
	"encoding/json"
	"github.com/christopherL91/TimeGolang/gin"
	"github.com/clbanning/mxj"
	"github.com/gorilla/websocket"
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	mgo_session *mgo.Session
	base_path   string
)

func init() {
	var err error
	base_path = os.Getenv("PARKOUR_BASE")
	user := os.Getenv("MONGO_USER")
	pass := os.Getenv("MONGO_PASS")
	host := os.Getenv("MONGO_HOST")
	port := os.Getenv("MONGO_PORT")
	mgo_session, err = mgo.Dial("mongodb://" + user + ":" + pass + "@" + host + ":" + port + "/parkour")
	if err != nil {
		panic(err)
	}
}

type Server struct {
	Connections map[*websocket.Conn]string
	Users       map[string][]*websocket.Conn
	broadcastCh chan map[string]interface{}
	messageCh   chan *Message
	removeCh    chan *websocket.Conn
	mutex       *sync.Mutex
}

func (s *Server) addConnection(conn *websocket.Conn, id string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.Connections[conn] = id
	connections, ok := s.Users[id]
	if ok {
		s.Users[id] = append(connections, conn)
	} else {
		s.Users[id] = []*websocket.Conn{conn}
	}
}

func (s *Server) removeConnection(conn *websocket.Conn) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.Connections, conn)
	id := s.Connections[conn]
	connections := s.Users[id]
	for i, c := range connections {
		if c == conn {
			//Delete connection at index i
			connections[i], connections = connections[len(connections)-1], connections[:len(connections)-1]
		}
	}
	s.Users[id] = connections
	log.Println("closed connection")
}

func (s *Server) sendMessage(data map[string]interface{}, conn *websocket.Conn) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := conn.WriteJSON(data); err != nil {
		s.removeCh <- conn
	}
}

func (s *Server) listen() {
	for {
		select {
		case message := <-s.broadcastCh:
			for connection := range s.Connections {
				s.sendMessage(message, connection)
			}
		case conn := <-s.removeCh:
			s.removeConnection(conn)
		case msg := <-s.messageCh:
			for _, user := range msg.To {
				conns := s.Users[user]
				for _, conn := range conns {
					s.sendMessage(msg.Body, conn)
				}
			}
		}
	}
}

func getBoutData(bout map[string]interface{}) map[string]interface{} {
	var users []map[string]interface{}
	var userids []bson.ObjectId
	userslice := bout["users"].([]interface{})
	for _, u := range userslice {
		id := u.(string)
		userids = append(userids, bson.ObjectIdHex(id))
	}
	mgo_session.DB("parkour").C("users").Find(bson.M{
		"_id": bson.M{
			"$in": userids,
		},
	}).All(&users)
	bout["users"] = users
	var course map[string]interface{}
	courseid := bout["course"].(string)
	mgo_session.DB("parkour").C("courses").FindId(bson.ObjectIdHex(courseid)).Select(bson.M{
		"name": 1,
		"code": 1,
	}).One(&course)
	bout["course"] = course
	return bout
}

func main() {
	secret := "I<3Unicorns"
	r := gin.Default()
	server := &Server{
		Connections: make(map[*websocket.Conn]string, 0),
		Users:       make(map[string][]*websocket.Conn, 0),
		broadcastCh: make(chan map[string]interface{}, 100),
		removeCh:    make(chan *websocket.Conn, 100),
		messageCh:   make(chan *Message, 100),
		mutex:       new(sync.Mutex),
	}

	go server.listen()

	public := r.Group("/api")
	private := r.Group("/api")
	private.Use(tokenMiddleWare(secret)) //verify with JWT

	public.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"hello": "hello world"})
	})

	public.GET("/login/kth", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "https://login.kth.se/login?service="+base_path+"/login/callback/kth")
	})

	public.GET("/login/callback/kth", func(c *gin.Context) {
		mgo_conn := mgo_session.Copy()
		defer mgo_conn.Close()

		ticket := c.Request.URL.Query().Get("ticket")
		client := new(http.Client)
		// kth_user := new(KTH_User)

		res, err := client.Get("https://login.kth.se/serviceValidate?ticket=" + ticket + "&service=" + base_path + "/login/callback/kth")
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

		log.Println(userid)
		out, err := exec.Command("/pkg/chpass/default/bin/pwsearch 1 " + userid + " 1 10").Output()
		log.Println("The users full name is: " + string(out))
		reg := regexp.MustCompile("([^:,]*)[:,]")
		matches := reg.FindAllString(string(out), 5)
		username := matches[0]
		name := matches[4]

		var profile map[string]interface{}
		err = json.Unmarshal(data, &profile)
		if err != nil {
			c.Fail(500, err)
			return
		}
		userData := map[string]interface{}{
			"kthid":     username,
			"firstname": strings.Split(name, " ")[0],
			"lastname":  strings.Split(name, " ")[1],
		}
		id := updateLoginInfo(mgo_conn, userData)
		payload := make(map[string]interface{})
		payload["name"] = profile["givenName"]
		payload["_id"] = id
		payload["exp"] = time.Now().Add(time.Hour * 24).Unix()
		token, err := generateToken([]byte(secret), &payload)
		if err != nil {
			c.Fail(500, err)
			return
		}
		c.JSON(200, gin.H{"token": token})
	})

	private.GET("/ws", func(c *gin.Context) {
		u, _ := c.Get("user")
		user := u.(map[string]interface{})
		conn, err := websocket.Upgrade(c.Writer, c.Request, nil, 1024, 1024)
		if err != nil {
			return
		}
		id := user["_id"].(string)
		server.addConnection(conn, id)
	})
	private.POST("/bouts", func(c *gin.Context) {
		u, _ := c.Get("user")
		user := u.(map[string]interface{})
		bout := new(Bout)
		if c.Bind(bout) {
			userid := user["_id"].(string)
			bout.Creator = userid
			bout.Users = append(bout.Users, userid)
			mgo_session.DB("parkour").C("bouts").Insert(bout)
		}
	})

	private.GET("/bouts", func(c *gin.Context) {
		u, _ := c.Get("user")
		user := u.(map[string]interface{})
		userid := user["_id"].(string)
		var result []map[string]interface{}
		err := mgo_session.DB("parkour").C("bouts").Find(bson.M{
			"users": bson.M{
				"$in": []string{userid},
			},
		}).Select(bson.M{
			"_id":    1,
			"users":  1,
			"course": 1,
			"lab":    1,
		}).All(&result)
		if err != nil {
			c.Fail(404, err)
			return
		}
		for _, bout := range result {
			bout = getBoutData(bout)
		}

		c.JSON(200, result)
	})

	private.GET("/bouts/:bout", func(c *gin.Context) {
		bout := c.Params.ByName("bout")
		boutid := bson.ObjectIdHex(bout)
		var result map[string]interface{}
		mgo_session.DB("parkour").C("bouts").FindId(boutid).One(&result)
		result = getBoutData(result)
		c.JSON(200, result)
	})

	/*
		{
			user: "id",
			action: "start"
		}
	*/
	private.POST("/bouts/:bout/logs", func(c *gin.Context) {
		bout := c.Params.ByName("bout")
		u, _ := c.Get("user")
		user := u.(map[string]interface{})
		boutlog := new(Log)
		if c.Bind(boutlog) {
			boutlog.Date = time.Now()
			id := user["_id"].(string)
			boutlog.User = id
			//Save boutlog to DB
			boutid := bson.ObjectIdHex(bout)
			err := mgo_session.DB("parkour").C("bouts").UpdateId(boutid, bson.M{
				"$push": bson.M{
					"logs": boutlog,
				},
			})
			if err != nil {
				log.Println(err.Error())
			}
			var result map[string]interface{}
			mgo_session.DB("parkour").C("bouts").FindId(boutid).One(&result)
			var users []string
			userslice := result["users"].([]interface{})
			for _, u := range userslice {
				userid := u.(string)
				users = append(users, userid)
			}
			server.messageCh <- &Message{
				To: users,
				Body: map[string]interface{}{
					"user":   id,
					"action": boutlog.Action,
				},
			}
		}
	})

	private.GET("/bouts/:bout/logs", func(c *gin.Context) {
		bout := c.Params.ByName("bout")
		boutid := bson.ObjectIdHex(bout)
		var result map[string]interface{}
		mgo_session.DB("parkour").C("bouts").FindId(boutid).One(&result)
		c.JSON(200, result["logs"])
	})

	r.Run(":3000")
}
