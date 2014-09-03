package main

import (
	"encoding/base64"
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
	"sync"
	"time"
)

var (
	mgo_session *mgo.Session
	base_path   string
	DB_name     string
)

func init() {
	var err error
	base_path = os.Getenv("PARKOUR_BASE")
	DB_name = os.Getenv("MONGO_DB_NAME")
	user := os.Getenv("MONGO_USER")
	pass := os.Getenv("MONGO_PASS")
	host := os.Getenv("MONGO_HOST")
	port := os.Getenv("MONGO_PORT")
	mgo_session, err = mgo.Dial("mongodb://" + user + ":" + pass + "@" + host + ":" + port + "/" + DB_name)
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
	mgo_conn := mgo_session.Copy()
	defer mgo_conn.Close()
	mgo_conn.SetMode(mgo.Monotonic, true)

	var users []map[string]interface{}
	var userids []bson.ObjectId
	userslice := bout["users"].([]interface{})
	for _, u := range userslice {
		id := u.(string)
		userids = append(userids, bson.ObjectIdHex(id))
	}
	err := mgo_conn.DB(DB_name).C("users").Find(bson.M{
		"_id": bson.M{
			"$in": userids,
		},
	}).All(&users)
	if err != nil {
		log.Fatalln(err.Error())
		return nil
	}

	bout["users"] = users
	var course map[string]interface{}
	courseid := bout["course"].(string)
	err = mgo_conn.DB(DB_name).C("courses").FindId(bson.ObjectIdHex(courseid)).Select(bson.M{
		"name": 1,
		"code": 1,
	}).One(&course)
	if err != nil {
		log.Fatalln(err.Error())
		return nil
	}
	bout["course"] = course
	return bout
}

func main() {
	secret := "I<3Unicorns"
	r := gin.Default()
	r.Use(CORSMiddleware())

	server := &Server{
		Connections: make(map[*websocket.Conn]string, 0),
		Users:       make(map[string][]*websocket.Conn, 0),
		broadcastCh: make(chan map[string]interface{}, 100),
		removeCh:    make(chan *websocket.Conn, 100),
		messageCh:   make(chan *Message, 100),
		mutex:       new(sync.Mutex),
	}

	//Start websocket server.
	go server.listen()

	//Public routes.
	public := r.Group("/api")
	//Private routes. Will need a token to be used with.
	private := r.Group("/api", tokenMiddleWare(secret))

	public.GET("/login/kth", func(c *gin.Context) {
		url := []byte(c.Request.URL.Query().Get("url"))
		c.Redirect(http.StatusTemporaryRedirect, "https://login.kth.se/login?service="+base_path+"/login/callback/kth/"+base64.StdEncoding.EncodeToString(url))
	})

	public.GET("/login/callback/kth/:callback", func(c *gin.Context) {
		mgo_conn := mgo_session.Copy()
		defer mgo_conn.Close()
		mgo_conn.SetMode(mgo.Monotonic, true)

		callback := c.Params.ByName("callback")
		decoded, _ := base64.StdEncoding.DecodeString(callback)
		callbackurl := string(decoded)
		ticket := c.Request.URL.Query().Get("ticket")
		client := new(http.Client)

		res, err := client.Get("https://login.kth.se/serviceValidate?ticket=" + ticket + "&service=" + base_path + "/login/callback/kth/" + callback)
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

		out, err := exec.Command("ldapsearch", "-x", "-LLL", "ugKthid="+userid).Output()
		if err != nil {
			c.Fail(500, err)
			return
		}
		reg := regexp.MustCompile("(?m)^([^:]+): ([^\n]+)$")
		matches := reg.FindAllStringSubmatch(string(out), -1)
		matchmap := make(map[string]string)
		for _, match := range matches {
			key := match[1]
			value := match[2]
			matchmap[key] = value
		}
		username := matchmap["uid"]
		name := matchmap["cn"]
		firstname := matchmap["givenName"]

		userData := map[string]interface{}{
			"kthid":     username,
			"firstname": firstname,
			"name":      name,
		}
		id := updateLoginInfo(mgo_conn, userData)
		payload := make(map[string]interface{})
		payload["name"] = userData["name"]
		payload["_id"] = id
		payload["exp"] = time.Now().Add(time.Hour * 24).Unix()
		token, err := generateToken([]byte(secret), &payload)
		if err != nil {
			c.Fail(500, err)
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, callbackurl+"?token="+token)
	})

	private.GET("/ws", func(c *gin.Context) {
		u, _ := c.Get("user")
		user := u.(map[string]interface{})
		conn, err := websocket.Upgrade(c.Writer, c.Request, nil, 1024, 1024)
		if err != nil {
			c.Fail(500, err)
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
			mgo_conn := mgo_session.Copy()
			defer mgo_conn.Close()
			mgo_conn.SetMode(mgo.Monotonic, true)

			userid := user["_id"].(string)
			bout.Creator = userid
			bout.Users = append(bout.Users, userid)
			err := mgo_conn.DB(DB_name).C("bouts").Insert(bout)
			if err != nil {
				c.Fail(500, err)
				return
			}
			c.JSON(200, gin.H{"status": "ok"})
		}
	})

	private.GET("/bouts", func(c *gin.Context) {
		mgo_conn := mgo_session.Copy()
		defer mgo_conn.Close()
		mgo_conn.SetMode(mgo.Monotonic, true)

		u, _ := c.Get("user")
		user := u.(map[string]interface{})
		userid := user["_id"].(string)
		var result []map[string]interface{}
		err := mgo_conn.DB("parkour").C("bouts").Find(bson.M{
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

		mgo_conn := mgo_session.Copy()
		defer mgo_conn.Close()
		mgo_conn.SetMode(mgo.Monotonic, true)

		boutid := bson.ObjectIdHex(bout)
		var result map[string]interface{}
		err := mgo_conn.DB("parkour").C("bouts").FindId(boutid).One(&result)
		if err != nil {
			c.Fail(500, err)
			return
		}
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
			mgo_conn := mgo_session.Copy()
			defer mgo_conn.Close()
			mgo_conn.SetMode(mgo.Monotonic, true)

			boutlog.Date = time.Now()
			id := user["_id"].(string)
			boutlog.ID = bson.NewObjectId()
			boutlog.Creator = id
			//Save boutlog to DB
			boutid := bson.ObjectIdHex(bout)
			err := mgo_conn.DB(DB_name).C("bouts").UpdateId(boutid, bson.M{
				"$push": bson.M{
					"logs": boutlog,
				},
			})
			if err != nil {
				c.Fail(500, err)
				return
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
					"creator": id,
					"action":  boutlog.Action,
				},
			}
			c.JSON(200, gin.H{"status": "ok"})
		}
	})

	/**
	 * Allow editing of latest log.
	 */
	private.PUT("/bouts/:bout/logs/:log", func(c *gin.Context) {
		//Not implemented
		mgo_conn := mgo_session.Copy()
		defer mgo_conn.Close()
		mgo_conn.SetMode(mgo.Monotonic, true)

		c.Abort(501)
		return
		boutid := c.Params.ByName("bout")
		logid := c.Params.ByName("log")
		//Replace with time from client
		date := time.Now()
		err := mgo_conn.DB("parkour").C("bouts").Update(bson.M{
			"_id":      bson.ObjectIdHex(boutid),
			"logs._id": bson.ObjectIdHex(logid),
		}, bson.M{
			"logs.$.date": date,
		})
		if err != nil {
			c.Fail(500, err)
			return
		}
		c.JSON(200, gin.H{"status": "ok"})
	})

	private.GET("/bouts/:bout/logs", func(c *gin.Context) {
		bout := c.Params.ByName("bout")

		mgo_conn := mgo_session.Copy()
		defer mgo_conn.Close()
		mgo_conn.SetMode(mgo.Monotonic, true)

		boutid := bson.ObjectIdHex(bout)
		var result map[string]interface{}
		err := mgo_conn.DB("parkour").C("bouts").FindId(boutid).One(&result)
		if err != nil {
			c.Fail(500, err)
			return
		}
		c.JSON(200, result["logs"])
	})

	private.GET("/users", func(c *gin.Context) {
		mgo_conn := mgo_session.Copy()
		defer mgo_conn.Close()
		mgo_session.SetMode(mgo.Monotonic, true)

		var users []User
		err := mgo_conn.DB(DB_name).C("users").Find(bson.M{}).All(&users)
		if err != nil {
			c.Fail(500, err)
			return
		}
		c.JSON(200, users)
	})

	private.GET("/users/:user", func(c *gin.Context) {
		userID := c.Params.ByName("user")
		mgo_conn := mgo_session.Copy()
		defer mgo_conn.Close()
		mgo_session.SetMode(mgo.Monotonic, true)

		user := new(User)
		err := mgo_conn.DB(DB_name).C("users").FindId(bson.ObjectIdHex(userID)).One(user)
		if err != nil {
			c.Fail(500, err)
			return
		}
		c.JSON(200, user)
	})

	r.Run(":3000")
}
