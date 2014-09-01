package main

import (
	"time"
)

type User struct {
	ID   string
	Name string
}

type Log struct {
	Date    time.Time `json:"date"`
	Action  string    `json:"action" binding:"required"`
	User    string    `json:"user" binding:"required"`
	Creator string    `json:"creator"`
}

type Bout struct {
	Users   []string `json:"users" binding:"required"`
	Logs    []*Log   `json:"logs"`
	Creator string   `json:"creator"`
	Course  string   `json:"course" binding:"required"`
	Lab     string   `json:"lab" binding:"required"`
}
