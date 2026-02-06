# Iziplay Anna's archive API

## Concepts

### Storage

For more simplicity, this uses a Postgres database (and Gorm as ORM under the hood) instead of the whole Anna's archive program

### API

This API provides some routes to let projects uses the Anna's archive database to search files

### Auto-sync

This app auto-sync 1 time per day (at launch or 24 hours after launch if it was sync before) by downloading Anna's archive metadata files if theses are new

## Why?

### No Anna's archive API

Anna's archive does not provide an API for search and I'm not very found of scraping. However, I love torrents!
