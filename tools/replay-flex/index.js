// Mock the driver at localhost:8382 to replay Senso Flex package recordings

const argv = require('minimist')(process.argv.slice(2))
const fs = require('fs')
const split = require('binary-split')
const websocket = require('ws')
const EventEmitter = require('events')

var recFile = argv['_'].pop() || 'rec/flex/zero.dat'
let speedFactor = 1/(parseFloat(argv['speed']) || 1)
let loop = !argv['once']

// Create a never ending stream of data
function Replayer (recFile) {
  var emitter = new EventEmitter()

  function createStream () {
    var stream = new fs.createReadStream(recFile).pipe(split())

    var timeout = 20
    stream.on('data', (data) => {
      stream.pause()

      var items = data.toString().split(',')
      var msg
      var timeout
      if (items.length === 2) {
        msg = items[1]
        timeout = items[0]
      } else {
        msg = items[0]
      }
      var buf = Buffer.from(msg, 'base64')
      emitter.emit('data', buf)

      setTimeout(() => {
        stream.resume()
      }, timeout * speedFactor)

    }).on('end', () => {
      if (loop) {
        console.log('End of the record stream, looping.')
        setTimeout(() => {
            createStream()
        }, timeout * speedFactor)
      } else {
        console.log('End of the record stream, exiting.')
        process.exit(0)
      }
    })
  }
  createStream()
  return emitter
}

const wss = new websocket.Server({ port: 8382 })

wss.on('connection', function connection(ws) {
  const dataStream = Replayer(recFile)

  dataStream.on('data', (data) => ws.send(data))
})
