// Mock Senso responses

// Firmware version to report in DEV_INFO
const VERSION_MAJOR = 2
const VERSION_MINOR = 0
const VERSION_FEATURE = 0
const VERSION_FIX = 0

// DATA_TYPES
const DATA_TYPE_GET_DEV_INFO = 0xD1
const DATA_TYPE_GET_VCC_INFO = 0xD2
const DATA_TYPE_SET_SPEED = 0xE5

module.exports = function (profile, data) {
  console.log(data)
  const body = data.slice(8)
  const type = body.readUInt16LE(2)
  switch (type) {
    case DATA_TYPE_GET_DEV_INFO:
      return {resp: devInfo(profile)}

    case DATA_TYPE_GET_VCC_INFO:
      return {resp: vccInfo()}

    case DATA_TYPE_SET_SPEED:
      const speedHz = body.slice(2+2).readUInt8();
      console.log("Setting speed to:", speedHz);
      return {cmd: {setSpeed: speedHz}, resp: standardResponse(type)};

    default:
      return {resp: standardResponse(type)}
  }
}

function devInfoItem (serial_number) {
  var devInfoItem = Buffer.alloc(32)

  // status code
  devInfoItem.writeUInt32LE(0, 0)

  // error code
  devInfoItem.writeUInt32LE(0, 4)

  // software version
  devInfoItem.writeInt8(VERSION_MAJOR, 11)
  devInfoItem.writeInt8(VERSION_MINOR, 10)
  devInfoItem.writeInt8(VERSION_FEATURE, 9)
  devInfoItem.writeInt8(VERSION_FIX, 8)

  // hardware version
  devInfoItem.writeUInt32LE(0, 12)

  // serial number
  devInfoItem.write(serial_number, 16, 16, 'ascii')

  return devInfoItem
}

function devInfo (profile) {
  var header = Buffer.alloc(8)

  var lenType = Buffer.alloc(4)
  lenType.writeUInt16LE(32 * 6, 0)
  lenType.writeUInt16LE(DATA_TYPE_GET_DEV_INFO | 0x8000, 2)

  return Buffer.concat([header,
    lenType,
    // The controller dev_info_item holds the device serial number and not the controller serial number!
    devInfoItem(profile.serial_number),
    devInfoItem(profile.board_serial_numbers.led_boards.center),
    devInfoItem(profile.board_serial_numbers.led_boards.up),
    devInfoItem(profile.board_serial_numbers.led_boards.right),
    devInfoItem(profile.board_serial_numbers.led_boards.down),
    devInfoItem(profile.board_serial_numbers.led_boards.left)
  ])
}

function vccInfo () {
  var header = Buffer.alloc(8)

  var lenType = Buffer.alloc(4)
  lenType.writeUInt16LE(12 * 6, 0)
  lenType.writeUInt16LE(DATA_TYPE_GET_VCC_INFO | 0x8000, 2)

  var vccInfo = Buffer.alloc(12)
  // vcc_3_3
  vccInfo.writeUInt16LE(3300, 0)
  // vcc5
  vccInfo.writeUInt16LE(5000, 2)
  // vcc_12_mot
  vccInfo.writeUInt16LE(12000, 4)
  // vcc_12_led
  vccInfo.writeUInt16LE(12000, 6)
  // vcc_19_led
  vccInfo.writeUInt16LE(19000, 8)
  // temperature
  vccInfo.writeInt16LE(0, 10)

  return Buffer.concat([
    header,
    lenType,
    // controller
    vccInfo,
    // 5 LED boards
    vccInfo,
    vccInfo,
    vccInfo,
    vccInfo,
    vccInfo
  ])
}

function standardResponse (type) {
  var header = Buffer.alloc(8)

  var response = Buffer.alloc(2 + 2 + 4 + 4 + 4)
  // write type with bit 15 set to indicate a response
  response.writeUInt16LE(type | 0x8000, 2)

  return Buffer.concat([header, response])
}
