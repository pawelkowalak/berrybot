[![CircleCI](https://circleci.com/gh/viru/berrybot.svg?style=svg)](https://circleci.com/gh/viru/berrybot)

# BerryBot

BerryBot is a simple driving robot based on Raspberry PI and DC motors. You can control it over WiFi using mobile application that connects to server process running on RPI. Both app and server are implemented in Go, using gRPC for communication.

## Hardware

**Chassis**: I'm using chassis below which come with 2 DC motors already. Good enough for your first robot.

* https://www.sparkfun.com/products/12091

**Raspberry PI** (I'm using rev 2, but 3 should be better due to built-in WiFi)

* https://www.raspberrypi.org/products/raspberry-pi-3-model-b/

**DC motors controller**. To connect DC motors to RPI, you need a controller with separate power source. You can't simply power up those motors directly from RPI. I'm using controller exactly as linked below (sorry, no English source for this), but you can find multiple alternatives that work exactly the same way.

* https://botland.com.pl/raspberry-pi-hat-kontrolery-silnikow-i-serw/3476-pimotor-dwukanalowy-sterownik-silnikow-nakladka-do-raspberry-pi.html

**Battery pack** is needed to give enough power for DC motors. I've tried using provided chassis for AA batteries, but motors barely moved. I'm using 1300mAh, 7.4V LiPo pack now and it lasts for 30mins of fun or few hours of idling.

* https://www.sparkfun.com/products/11856

**Distance sensors**. I'm using ultrasonic sensors, because they are cheap, but operating them is quite problematic (and require logic levels converter below).

* http://www.cytron.com.my/p-sn-hc-sr04

**Logic levels converter** is needed to connect ultrasonic distance sensors (using 5V) above to Raspberry Pi (using 3.3V). You can burn your RPI without it.

* http://www.cytron.com.my/p-lc04a

## Software

Generate proto:

`protoc -I ./proto/ ./proto/steering.proto --go_out=plugins=grpc:proto`

Install and run mobile app locally:

`go install github.com/viru/berrybot && berrybot`

Build and install mobile app on connected Android device:

`gomobile install github.com/viru/berrybot/berrycli`

Build and install mobile app on connected iOS device:

```sh
gomobile build -target=ios github.com/viru/berrybot
ios-deploy -b berrybot.app
```

Build server and upload to your RPI:

`GOOS=linux GOARCH=arm go build -o bbserver/bbserver ./bbserver/ && scp bbserver/bbserver pi:`

Run server on your RPI using sudo, because using GPIO pins requires it.
