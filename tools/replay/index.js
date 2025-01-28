// Mock up a Senso data and control server

const argv = require('minimist')(process.argv.slice(2))
const fs = require('fs')
const split = require('binary-split')
const net = require('net')
const bonjour = require('bonjour')()
const EventEmitter = require('events')

const control = require('./control')

var recFile = argv['_'].pop() || 'rec/senso/zero.dat'
let speedFactor = 1/(parseFloat(argv['speed']) || 1)
let loop = !argv['once']
let timeoutOverride = null;

function interpretCommand(cmd) {
    if (cmd && ('setSpeed' in cmd)) {
        timeoutOverride = 1000.0 / cmd['setSpeed']
    }
}

async function mockSenso (profile, data) {
  var socket = await listenForConnection('0.0.0.0', 55567)

  // Helper callback that can be removed
  function send (data) {
    socket.write(data)
  }

  data.on('data', send)
  socket.on('data', (incoming) => {
    const {cmd: cmd = null, resp: resp } = control(profile, incoming);

    interpretCommand(cmd);
    socket.write(resp);
  })

  socket.on('close', () => {
    console.log('Connection closed.')
    data.removeListener('data', send)
    mockSenso(profile, data)
  })

  socket.on('error', (err) => {
    console.log(err)
    data.removeListener('data', send)
    mockSenso(profile, data)
  })
}

function listenForConnection (host, port) {
  return new Promise((resolve, reject) => {
    console.log('Listening on ' + host + ':' + port)
    var server = net.createServer((socket) => {
      console.log('Connection: ' + socket.remoteAddress + ':' + socket.remotePort)

      // disable Nagle
      socket.setNoDelay()

      // Only allow one connection at a time
      server.close()
      resolve(socket)
    }).listen(port, host)
  })
}

// Create a never ending stream of data
function Replayer (recFile) {
  var emitter = new EventEmitter()

  function createStream () {
    var stream = new fs.createReadStream(recFile).pipe(split())

    stream.on('data', (data) => {
      stream.pause()

      var items = data.toString().split(',')
      var msg
      var timeout
      if (items.length === 2) {
        msg = items[1]
        timeout = items[0] * speedFactor
      } else {
        msg = items[0]
        timeout = 20 * speedFactor
      }
      if (timeoutOverride) {
          timeout = timeoutOverride;
      }
      }
      var buf = Buffer.from(msg, 'base64')
      emitter.emit('data', buf)

      setTimeout(() => {
        stream.resume()
      }, timeout)
    }).on('end', () => {
      if (loop) {
        console.log('End of the record stream, looping.')
        createStream()
      } else {
        console.log('End of the record stream, exiting.')
        process.exit(0)
      }
    })
  }
  createStream()
  return emitter
}

const profile = {
  serial_number: '31-00000000',
  board_serial_numbers: {
    controller: '30-00000000',
    led_boards: {
      'center': '30-00000001',
      'up': '30-00000002',
      'right': '30-00000003',
      'down': '30-00000004',
      'left': '30-00000005'
    }
  }
}

const dataStream = Replayer(recFile)

// Advertise Senso via mDNS
bonjour.publish({
  name: 'Senso data replayer',
  txt: {ser_no: profile.serial_number, mode: 'Application'},
  type: 'sensoControl',
  port: '55567'})

mockSenso(profile, dataStream)
