import { BufferUtility } from '../../../src/common/utility/buffer.utility';
import { Constants } from '../../../src/common/util/constants';

class BufferUtilityChild extends BufferUtility {
    constructor() {
        super();
    }
}
describe('BufferUtility', () => {
    describe('left padding', () => {

        it('Should add ', () => {
            // Arrange
            const buffer = Buffer.from([0x12, 0xfe, 0x0a]);
            const expectedHexDumpBuffer = '<HexBuffer (3-0x03) 0x12 0xfe 0x0a>';

            // Act
            const hexDumpBuffer = BufferUtility.hexDump(buffer);

            // Assert

            expect(hexDumpBuffer).toBe(expectedHexDumpBuffer);
        });
    });

    describe('decPadNumber', () => {
        it('Should return a padded number ', () => {
            // Arrange
            const expectedPaddedNumber = '012345';
            const aNumber = 12345;
            const paddingLength = 6;

            // Act
            const paddedNumber = BufferUtility.decPadNumber(aNumber, paddingLength);

            // Assert
            expect(paddedNumber).toBe(expectedPaddedNumber);
        });

        it('Should return a non padded number if size of number is >= padding length', () => {
            // Arrange
            const expectedPaddedNumber = '123456';
            const aNumber = 123456;
            const paddingLength = 6;

            // Act
            const paddedNumber = BufferUtility.decPadNumber(aNumber, paddingLength);

            // Assert
            expect(paddedNumber).toBe(expectedPaddedNumber);
        });
    });

    describe('decPadNumberStart', () => {
        it('Should return a padded number ', () => {
            // Arrange
            const expectedPaddedNumber = '-12345';
            const aNumber = 12345;
            const paddingLength = 6;
            const charpad = "-";

            // Act
            const paddedNumber = BufferUtility.decPadNumberStart(aNumber, paddingLength,charpad);

            // Assert
            expect(paddedNumber).toBe(expectedPaddedNumber);
        });

        it('Should return a non padded number if size of number is >= padding length', () => {
            // Arrange
            const expectedPaddedNumber = '123456';
            const aNumber = 123456;
            const paddingLength = 6;
            const charpad = "-";

            // Act
            const paddedNumber = BufferUtility.decPadNumberStart(aNumber, paddingLength,charpad);

            // Assert
            expect(paddedNumber).toBe(expectedPaddedNumber);
        });

        it('Should return a non padded number if size of number is >= padding length and crop to the spadding length', () => {
            // Arrange
            const expectedPaddedNumber = '123456';
            const aNumber = 1234567;
            const paddingLength = 6;
            const charpad = "-";

            // Act
            const paddedNumber = BufferUtility.decPadNumberStart(aNumber, paddingLength,charpad);

            // Assert
            expect(paddedNumber).not.toBe(expectedPaddedNumber);
        });
    });
    describe('decPadNumberEnd', () => {
        it('Should return a padded number ', () => {
            // Arrange
            const expectedPaddedNumber = '12345*';
            const aNumber = 12345;
            const paddingLength = 6;
            const charpad = "*";

            // Act
            const paddedNumber = BufferUtility.decPadNumberEnd(aNumber, paddingLength,charpad);

            // Assert
            expect(paddedNumber).toBe(expectedPaddedNumber);
        });

        it('Should return a non padded number if size of number is >= padding length', () => {
            // Arrange
            const expectedPaddedNumber = '123456';
            const aNumber = 123456;
            const paddingLength = 6;
            const charpad = "*";

            // Act
            const paddedNumber = BufferUtility.decPadNumberEnd(aNumber, paddingLength,charpad);

            // Assert
            expect(paddedNumber).toBe(expectedPaddedNumber);
        });

        it('Should return a non padded number if size of number is >= padding length and crop to the spadding length', () => {
            // Arrange
            const expectedPaddedNumber = '123456';
            const aNumber = 1234567;
            const paddingLength = 6;
            const charpad = "*";

            // Act
            const paddedNumber = BufferUtility.decPadNumberEnd(aNumber, paddingLength,charpad);

            // Assert
            expect(paddedNumber).not.toBe(expectedPaddedNumber);
        });
    });

    describe('decPadStringStart', () => {
        it('Should return a padded number ', () => {
            // Arrange
            const expectedPaddedNumber = '-12345';
            const aString = '12345';
            const paddingLength = 6;
            const charpad = "-";

            // Act
            const paddedNumber = BufferUtility.decPadStringStart(aString, paddingLength,charpad);

            // Assert
            expect(paddedNumber).toBe(expectedPaddedNumber);
        });

        it('Should return a non padded number if size of number is >= padding length', () => {
            // Arrange
            const expectedPaddedNumber = '123456';
            const aString = '123456';
            const paddingLength = 6;
            const charpad = "-";

            // Act
            const paddedNumber = BufferUtility.decPadStringStart(aString, paddingLength,charpad);

            // Assert
            expect(paddedNumber).toBe(expectedPaddedNumber);
        });

        it('Should return a non padded number if size of number is >= padding length and crop to the spadding length', () => {
            // Arrange
            const expectedPaddedNumber = '123456';
            const aString = '1234567';
            const paddingLength = 6;
            const charpad = "-";

            // Act
            const paddedNumber = BufferUtility.decPadStringStart(aString, paddingLength,charpad);

            // Assert
            expect(paddedNumber).not.toBe(expectedPaddedNumber);
        });
    });

    describe('decPadStringEnd', () => {
        it('Should return a padded number ', () => {
            // Arrange
            const expectedPaddedNumber = '12345*';
            const aString = '12345';
            const paddingLength = 6;
            const charpad = "*";

            // Act
            const paddedNumber = BufferUtility.decPadStringEnd(aString, paddingLength,charpad);

            // Assert
            expect(paddedNumber).toBe(expectedPaddedNumber);
        });

        it('Should return a non padded number if size of number is >= padding length', () => {
            // Arrange
            const expectedPaddedNumber = '123456';
            const aString = '123456';
            const paddingLength = 6;
            const charpad = "*";

            // Act
            const paddedNumber = BufferUtility.decPadStringEnd(aString, paddingLength,charpad);

            // Assert
            expect(paddedNumber).toBe(expectedPaddedNumber);
        });

        it('Should return a non padded number if size of number is >= padding length and crop to the spadding length', () => {
            // Arrange
            const expectedPaddedNumber = '123456';
            const aString = '1234567';
            const paddingLength = 6;
            const charpad = "*";

            // Act
            const paddedNumber = BufferUtility.decPadStringEnd(aString, paddingLength,charpad);

            // Assert
            expect(paddedNumber).not.toBe(expectedPaddedNumber);
        });
    });

    describe('hexDumpCount', () => {
        it('Should return a dec/hex representation of a Buffer byteCount', () => {
            // Arrange
            const buffer = Buffer.from([0x12, 0xfe, 0x0a]);
            const expectedHexDumpBuffer = '(3-0x03)';

            // Act
            const hexDumpBuffer = BufferUtility.hexDumpCount(buffer);

            // Assert
            expect(hexDumpBuffer).toBe(expectedHexDumpBuffer);
        });

        it('Should return a dec/hex representation of a number', () => {
            // Arrange
            const aNumber = 10;
            const expectedHexDumpBuffer = '(10-0x0a)';

            // Act
            const hexDumpBuffer = BufferUtility.hexDumpCount(aNumber);

            // Assert
            expect(hexDumpBuffer).toBe(expectedHexDumpBuffer);
        });

    });

    // TODO: add extract test when passing the start <> 0
    describe('replaceByte', () => {

        it('Should replaced an existing byte', () => {
            // Arrange
            const buffer = Buffer.from([0x12, 0xfe, 0x0a]);
            const byteToReplace = 0xfe;
            const newBytes = Buffer.from([0x01, 0x02, 0x03]);
            const expectedBuffer = Buffer.from([0x12, 0x01, 0x02, 0X03, 0x0a]);

            // Act
            const newBuffer = BufferUtility.replaceByte(buffer, byteToReplace, newBytes);
            console.log(BufferUtility.hexDump(newBuffer));

            // Assert
            expect(newBuffer.equals(expectedBuffer)).toBe(true);
        });

        it('Should replaced a not existing byte', () => {
            // Arrange
            const buffer = Buffer.from([0x12, 0xfe, 0x0a]);
            const byteToReplace = 0xaa;
            const newBytes = Buffer.from([0x01, 0x02, 0x03]);
            const expectedBuffer = buffer;

            // Act
            const newBuffer = BufferUtility.replaceByte(buffer, byteToReplace, newBytes);
            console.log(BufferUtility.hexDump(newBuffer));

            // Assert
            expect(newBuffer.equals(expectedBuffer)).toBe(true);
        });

        it('Should duplicate a  byte multiple times', () => {
            // Arrange
            const buffer = Buffer.from([0x12, 0xfe, 0x0a, 0xfe, 0x0b]);
            const byteToReplace = 0xfe;
            const newBytes = Buffer.from([0xfe, 0xfe]);
            const expectedBuffer = Buffer.from([0x12, 0xfe, 0xfe, 0x0a, 0xfe, 0xfe, 0x0b]);

            // Act
            const newBuffer = BufferUtility.replaceByte(buffer, byteToReplace, newBytes);
            console.log(BufferUtility.hexDump(newBuffer));

            // Assert
            expect(newBuffer.equals(expectedBuffer)).toBe(true);
        });
    });


    describe('calculateChecksum8', () => {

        it('Should calculate checksum 2s complement', () => {
            // Arrange
            const expectedChecksum = 0xe6;
            const buffer: Buffer = Buffer.from([0x12, 0xfe, 0x0a]);

            // Act
            const checksum = BufferUtility.calculateChecksum8(buffer);

            // Assert
            expect(checksum).toBe(expectedChecksum);
            expect(checksum - expectedChecksum).toBe(0);
        });
    });

    describe('combine2BytesMsbLsb', () => {

        it('Should combine bytes', () => {
            // Arrange
            const expectedByte = 0xad;
            const byte1 = 0x0a;
            const byte2 = 0x0d;

            // Act
            const newByte = BufferUtility.combine2BytesMsbLsb(byte1, byte2);

            // Assert
            expect(newByte).toBe(expectedByte);
        });
        it('Should combine bytes successfully with range limits fo the first argument is out of range', () => {
            // Arrange
            const expectedByte_0 = 0x0d;
            const expectedByte_15 = 0xfd;
            const byte1_0 = 0x00;
            const byte1_15 = 0x0f;
            const byte2 = 0x0d;

            // Act
            const newByte_0 = BufferUtility.combine2BytesMsbLsb(byte1_0, byte2);
            const newByte_15 = BufferUtility.combine2BytesMsbLsb(byte1_15, byte2);

            // Assert
            expect(newByte_0).toBe(expectedByte_0);
            expect(newByte_15).toBe(expectedByte_15);
        });
        it('Should throw an error when the first argument is out of range', () => {
            // Arrange
            const byte1_minus1 = -1;
            const byte1_25 = 0x19;
            const byte2 = 0x0d;

            // Act
            const newBytePromise_Minus0 = () => BufferUtility.combine2BytesMsbLsb(byte1_minus1, byte2);
            const newBytePromise_25 = () => BufferUtility.combine2BytesMsbLsb(byte1_25, byte2);

            // Assert
            expect(newBytePromise_Minus0).toThrowError(Error);
            expect(newBytePromise_25).toThrowError(Error);
        });
        it('Should combine bytes successfully with range limits fo the second argument is out of range', () => {
            // Arrange
            const expectedByte_0 = 0xa0;
            const expectedByte_15 = 0xaf;
            const byte1 = 0x0a;
            const byte2_0 = 0x00;
            const byte2_15 = 0x0f;

            // Act
            const newByte_0 = BufferUtility.combine2BytesMsbLsb(byte1, byte2_0);
            const newByte_15 = BufferUtility.combine2BytesMsbLsb(byte1, byte2_15);

            // Assert
            expect(newByte_0).toBe(expectedByte_0);
            expect(newByte_15).toBe(expectedByte_15);
        });
        it('Should throw an error when the second argument is out of range', () => {
            // Arrange
            const byte1 = 0x0a;
            const byte2_minus1 = -1;
            const byte2_25 = 0x19;

            // Act
            const newBytePromise_minus0 = () => BufferUtility.combine2BytesMsbLsb(byte1, byte2_minus1);
            const newBytePromise_25 = () => BufferUtility.combine2BytesMsbLsb(byte1, byte2_25);

            // Assert
            expect(newBytePromise_minus0).toThrowError(Error);
            expect(newBytePromise_25).toThrowError(Error);
        });
    });
});
