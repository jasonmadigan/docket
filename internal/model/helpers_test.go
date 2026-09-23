package model

import "time"

var t0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func at(hours int) time.Time { return t0.Add(time.Duration(hours) * time.Hour) }

func user(login string) Actor { return Actor{Login: login} }

func bot(login string) Actor { return Actor{Login: login, Bot: true} }
