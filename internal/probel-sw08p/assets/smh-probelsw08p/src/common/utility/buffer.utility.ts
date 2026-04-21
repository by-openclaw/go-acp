import * as _ from 'lodash';
import { SmartBuffer } from 'smart-buffer';
import { isNumber } from 'util';

import { ProtectTallyDumpCommandItems } from '../../command/tx/020-protect-tally-dump-message/items';
import { Constants } from '../util/constants';
import { Maybe } from '../util/type';

/**
 * Utility class providing functionalities on the `NodeJs Buffer` type uses for dealing with binary data directly
 *
 * @export
 * @abstract
 * @class BufferUtility
 */
export abstract class BufferUtility {
    private static readonly HEX_PADDING_CHAR = '0';
    private static readonly DEC_PADDING_CHAR = '0';

    /**
     * Throws an error when trying to instance of BufferUtility
     * Abstract class cannot be instantiates!
     * @memberof BufferUtility
     */
    protected constructor() {
        throw new Error(Constants.ABSTRACT_UTILITY_CLASS_CTOR_ERROR_MSG);
    }

    /**
     * Returns an hexadecimal representation of a Buffer
     * E.G : '<HexBuffer (3-0x03) 0x12 xfe 0x0a>'
     *
     * @static
     * @param {Buffer} buffer input binary data buffer
     * @returns {string} hexadecimal representation
     * @memberof BufferUtility
     */
    static hexDump(buffer: Maybe<Buffer>): string {
        if (buffer) {
            const view = new Uint8Array(buffer);
            const hex: string[] = Array.from(view).map((data: number): string => BufferUtility.hexPadNumber(data));
            return `<HexBuffer ${BufferUtility.hexDumpCount(buffer)} ${hex.join(' ')}>`;
        } else {
            return `<HexBuffer undefined>`;
        }
    }

    static hexDumpEmulator(buffer: Maybe<Buffer>): string {
        if (buffer) {
            const view = new Uint8Array(buffer);
            const hex: string[] = Array.from(view).map(
                (data: number): string =>
                    `${_.padStart(data.toString(16).toLowerCase(), 2, BufferUtility.HEX_PADDING_CHAR)}`
            );
            return `${hex.join(' ')}`;
        } else {
            return `undefined`;
        }
    }

    /**
     * Returns a decimal/hexadecimal representation of a Buffer bytes Count
     * E.G : '(3-0x03)'
     *
     * @static
     * @param {(Buffer | number)} data input binary data buffer | input number
     * @returns {string} decimal/hexadecimal Buffer bytes count representation
     * @memberof BufferUtility
     */
    static hexDumpCount(data: Buffer | number): string {
        const byteLength = isNumber(data) ? data : data.byteLength;
        return `(${byteLength}-${BufferUtility.hexPadNumber(byteLength)})`;
    }

    /**
     * Search for and replace matched single byte in byte(s) of another Buffer
     *
     * @static
     * @param {Buffer} buffer input binary data buffer
     * @param {number} byteToReplace  single by to search for
     * @param {Buffer} replacementBuffer bytes to use for the replacement
     * @param {number} [start=0] index of the first byte to start the replacement
     * @returns Search for and replace matched single byte in byte(s) of another Buffer
     * @memberof BufferUtility
     */
    static replaceByte(buffer: Buffer, byteToReplace: number, replacementBuffer: Buffer, start = 0): Buffer {
        const result = new SmartBuffer();
        if (start >= buffer.length) {
            throw new Error('start is out of range ');
        }

        for (let index = 0; index < buffer.length; index++) {
            const currentByte = buffer[index];
            if (index >= start && currentByte === byteToReplace) {
                result.writeBuffer(replacementBuffer);
            } else {
                result.writeUInt8(currentByte);
            }
        }
        return result.toBuffer();
    }

    /**
     * Returns byte where (4-7)=LSB[arg1] and (0-3)=LSB[arg2]
     * If args are out of range an Error is thrown.
     * Accepted range is [0,15]
     * @static
     * @param {number} msbOf it LSB part (bit 0-3) becomes the MSB of the result
     * @param {number} lsbOf it LSB part (bit 0-3) becomes the LSB of the result
     * @returns {number} a byte that combines 'msbOf' and 'lsbOf'
     * @memberof BufferUtility
     */
    static combine2BytesMsbLsb(msbOf: number, lsbOf: number): number {
        if (msbOf < 0 || msbOf > 15) {
            throw new Error(Constants.COMBINE_2_BYTES_MSB_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (lsbOf < 0 || lsbOf > 15) {
            throw new Error(Constants.COMBINE_2_BYTES_LSB_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        const buildMsbLsb = (msbOf << 4) | (lsbOf & 0x0f);
        return buildMsbLsb;
    }
    /**
     * Returns byte where (bits 4-6)=LSB[msbOf] and (bits 0-2)=LSB[lsbOf] (bits 4 & 7 = 0)
     * If msbOf or lsbOf are out of range an Error is thrown.
     * Accepted range is [0,895]
     * @static
     * @param {number} msbOf it LSB part becomes the MSB of the result
     * @param {number} lsbOf it LSB part becomes the LSB of the result
     * @returns {number} a byte that multipliers 'msbOf' and 'lsbOf'
     * @memberof BufferUtility
     */
    static combine2BytesMultiplierMsbLsb(msbOf: number, lsbOf: number): number {
        if (msbOf < 0 || Math.floor(msbOf / 128) > 7) {
            throw new Error(Constants.COMBINE_2_BYTES_MULTIPLIER_MSB_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (lsbOf < 0 || Math.floor(lsbOf / 128) > 7) {
            throw new Error(Constants.COMBINE_2_BYTES_MULTIPLIER_LSB_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        const buildMultiplier = ((Math.floor(msbOf / 128) << 4) & 0x70) | (Math.floor(lsbOf / 128) & 0x0f);
        return buildMultiplier;
    }

    /**
     * Calculate the checksum 2's complement of a Buffer
     *
     * @static
     * @param {Uint8Array} buffer input binary data buffer
     * @returns {number} 2's complement checksum
     * @memberof BufferUtility
     */
    static calculateChecksum8(buffer: Buffer): number {
        // Take the complement
        // Truncate - to get rid of the 8 bits to the right and keep the 8 LSB's
        return (~buffer.reduce((a, b) => a + b, 0) + 1) & 0xff;
    }

    /**
     * Pads number on the left side if it's shorter than length with zero and prefix it with '0x'
     * Padding characters are truncated if they exceed length.
     * E.G. '6' becomes '0x06'
     * @private
     * @static
     * @param {number} data input number
     * @returns {string}
     * @memberof BufferUtility
     */
    static hexPadNumber(data: number): string {
        return `0x${_.padStart(data.toString(16).toLowerCase(), 2, BufferUtility.HEX_PADDING_CHAR)}`;
    }

    /**
     * Pads number on the left side if it's shorter than length with zero
     * Padding characters are truncated if they exceed length.
     * E.G. '6' with length = 4 becomes '0006'
     *
     * @static
     * @param {number} data the number to pad
     * @param {number} length the padding length
     * @param {*} [chars=BufferUtility.DEC_PADDING_CHAR] string used for padding
     * @returns {string} the padded number
     * @memberof BufferUtility
     */
    static decPadNumber(data: number, length: number, chars = BufferUtility.DEC_PADDING_CHAR): string {
        return `${_.padStart(data.toString(), length, chars)}`;
    }

    /**
     * Pads character at the left side if it's shorter than length with char defined by the user
     * Padding characters are truncated if they exceed length.
     * E.G. '6' with length = 4 becomes '---6'
     *
     * @static
     * @param {number} data the number to pad
     * @param {number} length the padding length
     * @param {*} [chars=BufferUtility.DEC_PADDING_CHAR] string used for padding
     * @returns {string} the padded number
     * @memberof BufferUtility
     */
    static decPadNumberStart(data: number, length: number, char: string): string {
        return `${_.padStart(data.toString(), length, char)}`;
    }

    /**
     * Pads character at the end side if it's shorter than length with char defined by the user
     * Padding characters are truncated if they exceed length.
     * E.G. '6' with length = 4 becomes '6---'
     *
     * @static
     * @param {number} data the number to pad
     * @param {number} length the padding length
     * @param {string} char used for padding
     * @returns {string} the padded number
     * @memberof BufferUtility
     */
    static decPadNumberEnd(data: number, length: number, char: string): string {
        return `${_.padEnd(data.toString(), length, char)}`;
    }

    /**
     * Pads character at the left side if it's shorter than length with char defined by the user
     * Padding characters are truncated if they exceed length.
     * E.G. '6' with length = 4 becomes '---6'
     *
     * @static
     * @param {string} data the number to pad
     * @param {number} length the padding length
     * @param {*} [chars=BufferUtility.DEC_PADDING_CHAR] string used for padding
     * @returns {string} the padded number
     * @memberof BufferUtility
     */
    static decPadStringStart(data: string, length: number, char: string): string {
        return `${_.padStart(data, length, char)}`;
    }

    /**
     * Pads character at the end side if it's shorter than length with char defined by the user
     * Padding characters are truncated if they exceed length.
     * E.G. '6' with length = 4 becomes '6---'
     *
     * @static
     * @param {string} data the number to pad
     * @param {number} length the padding length
     * @param {string} char used for padding
     * @returns {string} the padded number
     * @memberof BufferUtility
     */
    static decPadStringEnd(data: string, length: number, char: string): string {
        return `${_.padEnd(data, length, char)}`;
    }

    /**
     * Gets an array of items split into groups the length of the specified size
     *
     * @static
     * @param {string[]} nameCharsItems the Name Chars Items array
     * @param {number} byteMaximumNumberOfNames the maximum number of Name Chars Items included in the group
     * @returns {string[][]} an array of items split into groups the length of the byteMaximumNumberOfNames
     * @memberof BufferUtility
     */
    static getChunkOfStringArray(nameCharsItems: string[], byteMaximumNumberOfNames: number): string[][] {
        // throw error if out of range
        // if (nameCharsItems.length < 1 || nameCharsItems.length > 65535) {
        //     throw new Error('nameCharsItems is out of range');
        // }

        // if (byteMaximumNumberOfNames < 1 || byteMaximumNumberOfNames > 32) {
        //     throw new Error('byteMaximumNumberOfNames is out of range');
        // }

        return _.chunk(nameCharsItems, byteMaximumNumberOfNames);
    }

    // TODO: be generic + unit test
    /**
     * Gets an array of items split into groups the length of the specified size
     *
     * @static
     * @param {ProtectTallyDumpCommandItems[]} deviceNumberProtectDataItems the device Number Protect Data Items array
     * @param {number} byteMaximumNumberOfTallies the maximum number of device Number Protect Data Items included in the group
     * @returns {ProtectTallyDumpCommandItems[][]} an array of items split into groups the length of the byteMaximumNumberOfTallies
     * @memberof BufferUtility
     */
    static getChunkOfNumberArray(
        deviceNumberProtectDataItems: ProtectTallyDumpCommandItems[],
        byteMaximumNumberOfTallies: number
    ): ProtectTallyDumpCommandItems[][] {
        return _.chunk(deviceNumberProtectDataItems, byteMaximumNumberOfTallies);
    }

    // TODO: be generic + unit test
    /**
     * gets a slice from an array from Index included to Index Excluded
     *
     * @static
     * @param {ProtectTallyDumpCommandItems[]} deviceNumberProtectDataItems the device Number Protect Data Items array
     * @param {number} fromIndexIncluded starting index is inclusive
     * @param {number} toIndexExcluded the ending is exclusive
     * @returns {ProtectTallyDumpCommandItems[]} an array
     * @memberof BufferUtility
     */
    static getSliceOfNumberArray(
        deviceNumberProtectDataItems: ProtectTallyDumpCommandItems[],
        fromIndexIncluded: number,
        toIndexExcluded: number
    ): ProtectTallyDumpCommandItems[] {
        return _.slice(deviceNumberProtectDataItems, fromIndexIncluded, toIndexExcluded);
    }
}
