#!/usr/bin/env python3
#
# Flex recording (`.dat) file to JSON converter.
#
# Input file is as output by recorder (see examples in rec/flex/*.dat), i.e.
# **only the frame samples** without any headers, etc.
#
# Output is a JSON with the following structure:
#
#   {
#       timestamps: [19, 43, 58, ...],
#       samples: [
#           {rows: [1, 3, 5],
#            cols: [4, 17, 1],
#            vals: [34, 200, 11]},
#           ...
#       ]
#   }
#
# where:
# - `zip(rows, cols, vals)` gives you `(row, col, force)` tuples
#   i.e. len(rows) == len(cols) == len(vals))
# - `zip(timestamps, samples)` gives you the sample series
#    i.e. len(timestamps) == len(samples)
# - timestamps are in millis (as output by recorder)
# - rows/cols/vals are as output by Flex firmware (0-23, 0-23 and 0-255)
# 
# Script assumes a Flex v4 protocol with 8bit depth.
# Unlike in Play, the script does not transform row/col indices!
# No error handling provided, the script will crash on data it cannot parse.
import struct
import argparse
import fileinput
import base64
from typing import List, Tuple
import json


def decode_frame(frame: bytes) -> List[Tuple[int, int, int]]:
    # Note: we DO NOT do `col -> dim - 1 - col` transformation
    # like in Play.
    return list(struct.iter_unpack("BBB", frame)) # B = unsigned char


def parse_body(body):
    binary_body = base64.b64decode(body)
    sensels = decode_frame(binary_body)
    # more compact and parsing-friendly format
    sample = {
        'rows': [row for (row, _, _) in sensels],
        'cols': [col for (_, col, _) in sensels],
        'vals': [val for (_, _, val) in sensels]
    }
    return sample


# we expect to receive milliseconds as ints
def parse_duration(dur):
    return int(dur)


def parse_flex_line(line):
    ll = line.split(",")
    dur = None
    body = None
    if len(ll) == 1:
        dur = "1"
        body = line
    else:
        dur = ll[0]
        body = ll[1]

    return (parse_duration(dur), parse_body(body))
        

def parse_flex_recording(inp):
    out = {
        'timestamps': [],
        'samples': []
    }
    timestamp = 0
    for line in inp:
        dur, sample = parse_flex_line(line)
        timestamp += dur
        out['timestamps'].append(timestamp)
        out['samples'].append(sample)

    return out


def parse_args():
    parser = argparse.ArgumentParser(
        description='Flex .dat to JSON converter')
    parser.add_argument("input_file",
        default=None,
        nargs='?',
        help="Flex .dat recording path (will assume stdin if not present)"
    )

    return parser.parse_args()


def main():
    args = parse_args()
    with fileinput.input(args.input_file) as inp:
        out = parse_flex_recording(inp)
        print(json.dumps(out))


if __name__ == "__main__":
    main()
