// Encoding

const BOARDS = {
    CENTER: 0,
    UP: 1,
    RIGHT: 2,
    DOWN: 3,
    LEFT: 4
}

const BOARD_ORDER = [BOARDS.CENTER, BOARDS.UP, BOARDS.RIGHT, BOARDS.DOWN, BOARDS.LEFT];

const HEADER = (() => {
    var buf = Buffer.allocUnsafe(1+1+6);
    buf.writeUInt8(1)  // version = 2
    buf.writeUInt8(1, 1) // block count = 1
    return buf;
})();

const TYPE_CODE = (() => {
    var buf = Buffer.allocUnsafe(2+1+1);
    buf.writeUInt16LE(0) // no idea
    buf.writeUInt8(0x80, 2) // data_type_response
    buf.writeUInt8(0, 3) // no idea
    return buf;
})();


function uint32LE(f) {
    const buf = Buffer.allocUnsafe(4)
    buf.writeUInt32LE(f)
    return buf;
}

function floatToInt16LE(f) {
    // TODO: validate the float
   const buf = Buffer.allocUnsafe(2)
   buf.writeInt16LE(f)
   return buf
}

function encodeTimestamp(t) {
    return uint32LE(t)
}

function binaryConcat(elems) {
    return Buffer.concat(elems)
}

function encodeBoard({a, b, c, d}) {
    return binaryConcat([a, b, c, d].map(floatToInt16LE))
}

function encodeBoards(boardMap) {
    return binaryConcat(BOARD_ORDER.map(b => boardMap[b]).map(encodeBoard))
}

function encodeAndWrapBoardMap(t, boardMap) {
    return binaryConcat([HEADER, TYPE_CODE, encodeTimestamp(t), encodeBoards(boardMap)]);
}

// Coordinate transforms
//
// There are 3 coordinate systems used here:
// - normal senso coords - [0,3]x[0,3]
// - CenterCoords - centered and y-axis flipped, see sensoCoordsToCenterCoords
//                  used for computing angles/identifying boards
// - BoardCoords - [0,1]x[0,1] for center and "trapezoid" for other boards, see sensoCoordsToBoardCoords
//                 used for inverting CoP to sensor readings

// Maps [0, 3]^2 into [-1.5, 1.5]^2, with *** Y AXIS FLIPPED ***
// so that positive is always up/right, negative is down/left.
//        0   1   2   3           -1.5    0     1.5
//      0 ╔═══════════╗         1.5 ╔═══════════╗
//        ║ ⟍       ⟋ ║             ║ ⟍       ⟋ ║
//      1 ║   ┌───┐   ║             ║   ┌───┐   ║
//        ║   │   │   ║ --- >     0 ║   │ 0 │   ║
//      2 ║   └───┘   ║             ║   └───┘   ║
//        ║ ⟋       ⟍ ║             ║ ⟋       ⟍ ║
//      3 ╚═══════════╝        -1.5 ╚═══════════╝
//
function sensoCoordsToCenterCoords(x, y) {
    return {x: x - 1.5, y: (y - 1.5) * -1}
}

// maps center coords to angle in radians
// 0 = pointing right
// all returned angle values are positive
function centerCoordsToAngle(x, y) {
    var rads = Math.atan2(y, x);
    if (rads < 0) {
        rads = 2*Math.PI + rads;
    }
    return rads
}

function radiansToBoard(rads) {
    if (centerCoordsToAngle(1, 1) <= rads && rads < centerCoordsToAngle(-1, 1)) {
        return BOARDS.UP;
    }
    else if (centerCoordsToAngle(-1, 1) <= rads && rads < centerCoordsToAngle(-1, -1)) {
        return BOARDS.LEFT;
    }
    else if (centerCoordsToAngle(-1, -1) <= rads && rads < centerCoordsToAngle(1, -1)) {
        return BOARDS.DOWN;
    }
    else {
        return BOARDS.RIGHT;
    }
}

// map senso coordinates ([0, 3] x [0, 3]) to a specific board number
function boardFromSensoCoords(x, y) {
    const {x: cX, y: cY} = sensoCoordsToCenterCoords(x, y);
    if (Math.abs(cX) < 0.5 && Math.abs(cY) < 0.5) {
        return BOARDS.CENTER;
    }
    else {
        const angle = centerCoordsToAngle(cX, cY);
        return radiansToBoard(angle);
    }
}


// Center has coords [0, 1] x [0, 1] where (0,0) is "c" sensor
// Other boards have "trapezoid" coords:
//  { (x, y) in [-1.5, 1.5]x[0,1] where abs(x) < 0.5 + y}
// where the orientation is always the same (e.g. "b" sensor (1.5, 1)
function sensoCoordsToBoardCoords(x, y) {
    const board = boardFromSensoCoords(x, y);
    if (board === BOARDS.CENTER) {
        return {x: x - 1, y: -y + 2}
    }
    else {
        const {x: cX, y: cY} = sensoCoordsToCenterCoords(x, y);
        var bX;
        var bY;
        // first we sort out the orientation
        if (board === BOARDS.LEFT) {
            bX = cY;
            bY = -cX;
        }
        else if (board === BOARDS.RIGHT) {
            bX = -cY;
            bY = cX;
        }
        else if (board === BOARDS.UP) {
            bX = cX;
            bY = cY;
        }
        else if (board === BOARDS.DOWN) {
            bX = -cX;
            bY = -cY;
        }
        // Now we are oriented like in the UP board, it remains to just shift
        // the origin.
        // For X coordinates, we do nothing
        // For Y coordinates, we shift [0.5, 1.5] to [0, 1]
        return {x: bX, y: bY - 0.5}
    }
}


// CoP {x, y, f} to Sensor value inversion {a, b, c, d}

// Given a guess of the `d` sensor's value as proportion
// of the total force:
//
//      d = k*f (where k in [0,1])
//
// produces the readings of all sensors.
function computeFromK(board, k, {x, y, f}) {
        const d = f * k

        // equations obtained via wolfram alpha
        if (board == BOARDS.CENTER) {
            // Solve {a+b+c+d=M, a + b = y*M, b+d = x*M} for {a,b,c}
            return {
                a: d + f*(y-x),
                b: f*x - d,
                c: f - d - f*y,
                d: d
            }
        }
        else {
            // Solve {a+b+c+d=M, a + b = y*M, -1.5*a - 0.5c + 1.5*b+0.5d = x*M} for {a,b,c}
            return {
                a: 1/6 * ( 2*d + f * (-2*x + 4 * y - 1)),
                b: 1/6 * (-2*d + f * (2*x + 2*y + 1)),
                c: -1*d - f*y + f,
                d: d
            }
        }
}

// sums negative readings, output is POSITIVE
function negSum({a, b, c, d}) {
    return [a, b, c, d].filter(x => x < 0).reduce((acc, v) => acc + v, 0) * -1
}

// Given a CoP {x, y} and a sum-force of `f`, this aims to find sensor values
// that would produce it by looping through possible sensor `d` values set to
// `d = k*f` for k in [0,1].
// It is probably possible to solve this analytically, but I don't know how.
// TODO: optimize this by first trying to pick values close to k
// e.g. initial_guess = dist({x:0.5, y: 0}, {x, y}) / sum_i(dist(i, {x,y})
function findK(board, {x, y, f}) {
    const precision = 100;
    var bestSoFar = {negSum: 100000, p: null}
    for (var i=0; i<precision; i++) {
        const k = i / precision
        const potential = computeFromK(board, k, {x, y, f})
        const potentialNextSum = negSum(potential)
        if (potentialNextSum < bestSoFar['negSum']) {
            bestSoFar = {negSum: potentialNextSum, p: potential}
        }
        if (bestSoFar['negSum'] < f/100) {
            // good enough, break loop
            break
        }
    }

    return bestSoFar.p
}

// {x, y} is in Board Coords!
function boardCordActivationToSensorValues(board, {x, y, f}) {
    const {a, b, c, d} = findK(board, {x, y, f})
    return {
        a: Math.max(0, a),
        b: Math.max(0, b),
        c: Math.max(0, c),
        d: Math.max(0, d),
    }
}


// Input is in senso coords
function activationToSensorValues({x, y, f}) {
    const board = boardFromSensoCoords(x, y);
    const {x: bX, y: bY} = sensoCoordsToBoardCoords(x, y);
    const values = boardCordActivationToSensorValues(board, {x: bX, y: bY, f});
    return {board: board, values: values}
}

// Converts a list of {x, y, f} activations into
// a board map (boards[board] = sensorValues).
// - an empty list produces zeroes in all boards
// - "last activation wins" if they map to the same board
function activationsToBoardMap(activations) {
    const boards = {}
    // initialize with zeroes
    const zeroes = {a: 0, b: 0, c: 0, d: 0};
    for (const board in BOARD_ORDER) {
        boards[board] = zeroes
    }
    //
    for (const activation of activations) {
        const {board, values} = activationToSensorValues(activation)
        boards[board] = values
    }

    return boards
}

function activationsToMessage(t, activations) {
    const boardMap = activationsToBoardMap(activations)
    return encodeAndWrapBoardMap(t, boardMap)
}

function squareWave(v, period, proportionOff) {
    if (v % period < period / proportionOff) {
        return 0
    }
    else {
        return 1
    }
}

// t is in millis
function genData(t) {
    const speed = 100 // "speed factor", 1/ms

    const freq = 1000*1000 / speed

    const forceMult = 2000

    // progress, 0 <= p < 1
    const p = (t % freq) / freq

    const stepMultiplier = 1
    //const stepMultiplier = squareWave(t, 1000, 5)

    return activationsToMessage(
        t,
        [
            // big outer circle
            {
                x: 1.5 + Math.cos(p*Math.PI*2),
                y: 1.5 + Math.sin(p*Math.PI*2),
                f: forceMult * (5 + 5*(1+Math.cos(p*Math.PI*2))) * stepMultiplier
            },
            // small inner circle
            {
                x: 1.5 + 0.3*Math.cos(p*Math.PI*-2),
                y: 1.5 + 0.3*Math.sin(p*Math.PI*-2),
                f: forceMult * (7 + 3*(1+Math.sin(6*p*Math.PI*2))) * stepMultiplier
            },
        ]);
}


module.exports = {
    'activationsToMessage': activationsToMessage,
    'genData': genData
}
