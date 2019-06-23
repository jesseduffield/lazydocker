# lazydocker (https://circleci.com/gh/jesseduffield/lazydocker) [![Go Report Card](https://goreportcard.com/badge/github.com/jesseduffield/lazydocker)](https://goreportcard.com/report/github.com/jesseduffield/lazydocker) [![GolangCI](https://golangci.com/badges/github.com/jesseduffield/lazydocker.svg)](https://golangci.com) [![GoDoc](https://godoc.org/github.com/jesseduffield/lazydocker?status.svg)](http://godoc.org/github.com/jesseduffield/lazydocker) [![GitHub tag](https://img.shields.io/github/tag/jesseduffield/lazydocker.svg)]()
[![CircleCI](https://circleci.com/gh/jesseduffield/lazydocker.svg?style=svg)]

A simple terminal UI for docker, written in Go with the [gocui](https://github.com/jroimartin/gocui 'gocui') library.

<p align="center">
  <img src="https://user-images.githubusercontent.com/8456633/59972109-8e9c8480-95cc-11e9-8350-38f7f86ba76d.png">
</p>

Are YOU tired of this workflow:

- recognise your local server isn't responding
- run `docker ps`
- see your container exited
- place mouse at beginning of the container ID, drag to the end of the container ID
- press cmd+c
- run 'docker restart <cmd+v>'

sounds like you might want to give lazydocker a try!

![Gif](/docs/resources/lazydocker-example.gif)

- [Installation](https://github.com/jesseduffield/lazydocker#installation)
- [Usage](https://github.com/jesseduffield/lazydocker#usage),
  [Keybindings](/docs/keybindings)
- [Cool Features](https://github.com/jesseduffield/lazydocker#cool-features)
- [Contributing](https://github.com/jesseduffield/lazydocker#contributing)
- [Video Tutorial](https://youtu.be/VDXvbHZYeKY)
- [Twitch Stream](https://www.twitch.tv/jesseduffield)

## Installation

### Homebrew

```sh
brew tap jesseduffield/lazydocker
brew install lazydocker
```

### Binary Release (Linux/OSX)

You can download a binary release [here](https://github.com/jesseduffield/lazydocker/releases).

### Go

```sh
go get github.com/jesseduffield/lazydocker
```

## Usage

Call `lazydocker` in your terminal. I personally use this a lot so I've made an alias for it like so:

```
echo "alias ld='lazydocker'" >> ~/.zshrc
```

(you can substitute .zshrc for whatever rc file you're using)

- Basic video tutorial [here](https://youtu.be/VDXvbHZYeKY).
- List of keybindings
  [here](/docs/keybindings).

## Cool features

everything is one keypress away:

- viewing the state of your docker or docker-compose container environment at a glance
- viewing logs for a container/service
- viewing cool ascii graphs of any container with any metric you want to measure (e.g. CPU percentage, memory usage)
- attaching to a container/service
- restarting/removing/rebuilding containers/services
- viewing the ancestor layers of a given image
- pruning containers, images, or volumes that are hogging up disk space

## Contributing

We love your input! Please check out the [contributing guide](CONTRIBUTING.md).
For contributor discussion about things not better discussed here in the repo, join the slack channel

[![Slack](/docs/resources/slack_rgb.png)](https://join.slack.com/t/lazydocker/shared_invite/enQtNDE3MjIwNTYyMDA0LTM3Yjk3NzdiYzhhNTA1YjM4Y2M4MWNmNDBkOTI0YTE4YjQ1ZmI2YWRhZTgwNjg2YzhhYjg3NDBlMmQyMTI5N2M)

## Donate

If you would like to support the development of lazydocker, please donate

[![Donate](https://d1iczxrky3cnb2.cloudfront.net/button-medium-blue.png)](https://donorbox.org/https://donorbox.org/lazydocker)

## Social

If you want to see what I (Jesse) am up to in terms of development, follow me on
[twitter](https://twitter.com/DuffieldJesse) or watch me program on
[twitch](https://www.twitch.tv/jesseduffield)

## Alternatives

- [docui](https://github.com/skanehira/docui) (Skanehira beat me to the punch on making a docker terminal UI, so definitely check out that repo as well! I think the two repos can live in harmony though: lazydocker is more about managing existing containers/services, and docui is more about creating and configuring them).
