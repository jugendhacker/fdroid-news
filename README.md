# fdroid-news
fdroid-news is a XMPP bot. It could post news about updates of [F-Droid](https://f-droid.org/) repositories to an [XMPP](https://xmpp.org/) MUC.

## Requirements
To install fdroid-news you need to have the go toolchain installed. Please consider your package manager on how to install it.

## Installation
If you have the go toolchain installed simply run

```bash
$ go install git.sr.ht/~j-r/fdroid-news@latest
```

This installs the `fdroid-news` binary into `$GOPATH/bin/`, put this folder into your path or run the program with the absolute path.

As a next step modify config.yml.example by your needs and put it somewhere into your filesystem. Next cd into that folder and run

```bash
$ fdroid-news -c config.yml
```

And here you go, the bot starts.

**Please be sure to not remove `fdroid-news.sqlite` in the current folder, because this file contains the database of the bot!**