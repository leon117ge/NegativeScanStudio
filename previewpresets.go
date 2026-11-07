package main

import "basetool/filmdb"

func calibratedStocks() []filmdb.Entry {
	db, err := filmdb.Load(filmdb.Path(configDir()))
	if err != nil {
		return nil
	}
	return db.Entries
}
