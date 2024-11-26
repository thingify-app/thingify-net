# thingify-net

This application creates a virtual network interface which relays IP packets
to/from another `thing-rtc` peer.

This allows network-based applications running on the installed host to
interact with e.g. a network stack running in a browser via WebRTC.

## Build

Ensure [Go is installed](https://go.dev/doc/install).

If building on a device with a camera, build with the camera tag:

```bash
go build -tags with_camera
```

Otherwise, a test screen can be substituted for the video feed, by building with:

```bash
go build
```

The resulting binary, `thingify-net`, will be produced in the current directory.

## Running

### Pairing

The first step to running `thingify-net` is to pair it with a browser. Navigate
to https://thingify.app and click "Pair". Take note of the shortcode displayed
below the QR code.

Next, complete the pairing by associating the local `thingify-net` instance with
this shortcode:

```bash
sudo ./thingify-net pair --shortcode <shortcode>
```

### Connecting

Pairing only needs to be completed once. Thereafter, you can run `thingify-net`
locally:

```bash
sudo ./thingify-net connect --withMedia
```

It is run as root to allow it to create a network interface. This could also be
achieved using less privileges with Linux capabilities.

The `--withMedia` flag specifies that it should stream video, either from the
camera (if built with `with_camera`) or from the test pattern.

This will create a network interface named `thingify0`, which can be seen with
e.g. `ifconfig`. The remote browser peer currently has a hardcoded IP address of
`10.0.1.2`, so any packets sent to/from this IP address will be routed to the
browser.

## Use to relay MAVLink packets

To use the MAVLink GCS UI available at https://thingify.app, connect a Flight
Controller to the machine running `thingify-net` via USB (e.g. a Raspberry Pi).

To relay MAVLink packets to/from this USB device and the browser, we can use
[MAVProxy](https://ardupilot.org/mavproxy/) and point it to the remote browser
IP address (hardcoded as 10.0.1.2 currently). This assumes the USB serial device
corresponding to the Flight Controller is `/dev/ttyACM0`:

```bash
mavproxy.py --master=/dev/ttyACM0 --out 10.0.1.2 --daemon --state-basedir="/home/pi" --cmd="set heartbeat 0;"
```

## Start automatically at boot

On a system with `systemd` (like Raspberry Pi OS or other Debian variants),
`thingify-net` and `mavproxy` can be configured to start automatically at
system boot, and also restart if they crash.

### MAVProxy

To start `mavproxy` at boot, create a "unit file" named `mavproxy.service`
(this assumes it is installed in `/home/pi/.local/bin` and the `pi` user
exists):

```
[Unit]
Description=MavProxy service
After=network.target

[Service]
ExecStart=/usr/bin/env /home/pi/.local/bin/mavproxy.py --master=/dev/ttyACM0 --out 10.0.1.2 --daemon --state-basedir="/home/pi" --cmd="set heartbeat 0;"
Restart=always
RestartSec=3
User=pi

[Install]
WantedBy=multi-user.target
```

Copy this file to `/etc/systemd/system` and set its permissions:

```
sudo cp mavproxy.service /etc/systemd/system/mavproxy.service
sudo chmod 644 /etc/systemd/system/mavproxy.service
```

Start the service and enable it to start at boot:

```
sudo systemctl start mavproxy
sudo systemctl enable mavproxy
```

To view logs, run:

```
journalctl -u mavproxy.service -f
```

### thingify-net

Ensure `thingify-net` is paired by running as the `pi` user, i.e. without
`sudo`:

```
./thingify-net pair --shortcode <shortcode>
```

Copy the `thingify-net` binary to `/usr/local/bin`:

```
sudo cp thingify-net /usr/local/bin
```

Create a "unit file" named `thingify.service`:

```
[Unit]
Description=Thingify

[Service]
ExecStart=/usr/local/bin/thingify-net connect --withMedia
User=pi
AmbientCapabilities=CAP_NET_ADMIN
Restart=always
# Rate limit restarts, which also avoids the process being permanently killed.
RestartSec=3

[Install]
WantedBy=multi-user.target
```

Note that this runs `thingify-net` as the `pi` user (assumed present on
Raspberry Pi). To allow it to create a network interface without being root, it
grants the `CAP_NET_ADMIN` capability.

As above, copy this file to `/etc/systemd/system` and set its permissions:

```
sudo cp thingify.service /etc/systemd/system/thingify.service
sudo chmod 644 /etc/systemd/system/thingify.service
```

Start the service and enable it to start at boot:

```
sudo systemctl start thingify
sudo systemctl enable thingify
```

To view logs, run:

```
journalctl -u thingify.service -f
```
