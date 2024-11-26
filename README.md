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
./thingify-net pair --shortcode <shortcode>
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
