import * as _ from 'lodash';
import { BufferUtility } from '../../../src/common/utility/buffer.utility';
import { Constants } from '../../../src/common/util/constants';

class BufferUtilityChild extends BufferUtility {
    constructor() {
        super();
    }
}
describe('BufferUtility', () => {
    describe('ctor', () => {
        it('Should throw an error (abstract class))', () => {
            // Arrange

            // Act
            const bufferPromise = () => new BufferUtilityChild();

            // Assert
            expect(bufferPromise).toThrowError(Error);
            expect(bufferPromise).toThrowError(Constants.ABSTRACT_UTILITY_CLASS_CTOR_ERROR_MSG);
        });
    });

    describe('hexDump', () => {
        it('Should dump a Buffer in hexadecimal', () => {
            // Arrange
            const buffer = Buffer.from([0x12, 0xfe, 0x0a]);
            const expectedHexDumpBuffer = '<HexBuffer (3-0x03) 0x12 0xfe 0x0a>';

            // Act
            const hexDumpBuffer = BufferUtility.hexDump(buffer);

            // Assert

            expect(hexDumpBuffer).toBe(expectedHexDumpBuffer);
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
            const expectedBuffer = Buffer.from([0x12, 0x01, 0x02, 0x03, 0x0a]);

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

    describe('combine2BytesMultiplierMsbLsb', () => {
        it('Should multiplier two bytes', () => {
            // Arrange
            const expectedByte = 0x00;
            const byte1 = 0x0a;
            const byte2 = 0x0d;

            // Act
            const newByte = BufferUtility.combine2BytesMultiplierMsbLsb(byte1, byte2);

            // Assert
            expect(newByte).toBe(expectedByte);
        });
        it('Should mulitplier bytes successfully with range limits fo the first argument is out of range', () => {
            // Arrange
            const expectedByte_0 = 0x00;
            const expectedByte_15 = 0x70;
            const byte1_0 = 0x00;
            const byte1_15 = 0x3ff; //1023
            const byte2 = 0x0d;

            // Act
            const newByte_0 = BufferUtility.combine2BytesMultiplierMsbLsb(byte1_0, byte2);
            const newByte_15 = BufferUtility.combine2BytesMultiplierMsbLsb(byte1_15, byte2);

            // Assert
            expect(newByte_0).toBe(expectedByte_0);
            expect(newByte_15).toBe(expectedByte_15);
        });
        it('Should throw an error when the first argument is out of range', () => {
            // Arrange
            const byte1_minus1 = -1;
            const byte1_1024 = 0x400; // 1024
            const byte2 = 0x0d;

            // Act
            const newBytePromise_Minus0 = () => BufferUtility.combine2BytesMultiplierMsbLsb(byte1_minus1, byte2);
            const newBytePromise_25 = () => BufferUtility.combine2BytesMultiplierMsbLsb(byte1_1024, byte2);

            // Assert
            expect(newBytePromise_Minus0).toThrowError(Error);
            expect(newBytePromise_25).toThrowError(Error);
        });
        it('Should combine bytes successfully with range limits fo the second argument is out of range', () => {
            // Arrange
            const expectedByte_0 = 0x00;
            const expectedByte_15 = 0x07;
            const byte2_0 = 0x00;
            const byte2_15 = 0x3ff; //1023
            const byte1 = 0x0d;

            // Act
            const newByte_0 = BufferUtility.combine2BytesMultiplierMsbLsb(byte1, byte2_0);
            const newByte_15 = BufferUtility.combine2BytesMultiplierMsbLsb(byte1, byte2_15);

            // Assert
            expect(newByte_0).toBe(expectedByte_0);
            expect(newByte_15).toBe(expectedByte_15);
        });
        it('Should throw an error when the second argument is out of range', () => {
            // Arrange
            const byte2_minus1 = -1;
            const byte2_1024 = 0x400; // 1024
            const byte1 = 0x0d;

            // Act
            const newBytePromise_Minus0 = () => BufferUtility.combine2BytesMultiplierMsbLsb(byte1, byte2_minus1);
            const newBytePromise_25 = () => BufferUtility.combine2BytesMultiplierMsbLsb(byte1, byte2_1024);

            // Assert
            expect(newBytePromise_Minus0).toThrowError(Error);
            expect(newBytePromise_25).toThrowError(Error);
        });
    });

    describe('Chunk an array of string with padding', () => {
        // it('Should thrown error - byteMaximumNumberOfNames is out of range', () => {
        //     // // Arrange

        //     const byteToLengthSample = 4;
        //     const byteMaximumNumberOfNamesSample = 0;


        //     // // Act

        //     let nameCharsItemsSample = new Array<string>();
        //     for (let nbr = 0; nbr < 4; nbr++) {
        //         nameCharsItemsSample.push(BufferUtility.decPadStringEnd(nbr.toString(), byteToLengthSample, '0'));
        //     }

        //     // The _.chunk function creates an array of elements split into groups the length of the specified size.
        //     let chunck1 = BufferUtility.getChunckOfStringArray(nameCharsItemsSample, byteMaximumNumberOfNamesSample);

        //     // Assert
        //     expect( ).toThrow(Error);
        // });

        // it('Should thrown error - namecharItems is out of range', () => {
        //     // // Arrange

        //     const byteToLengthSample = 4;
        //     const byteMaximumNumberOfNamesSample = 32;


        //     // // Act

        //     let nameCharsItemsSample = new Array<string>();
        //     for (let nbr = 0; nbr < 0; nbr++) {
        //         nameCharsItemsSample.push(BufferUtility.decPadStringEnd(nbr.toString(), byteToLengthSample, '0'));
        //     }

        //     // The _.chunk function creates an array of elements split into groups the length of the specified size.
        //     let chunck1 = BufferUtility.getChunckOfStringArray(nameCharsItemsSample, byteMaximumNumberOfNamesSample);

        //     // Assert
        //     expect( ).toThrow(Error);
        // });

        it('Should chunk an array of string with 64 items, Max items per group is 32 and End Padding with 0', () => {
            // // Arrange

            const byteToLengthSample = 4;
            const byteMaximumNumberOfNamesSample = 32;
            const expectedChunck1_1_30 = '6200';
            const expectedChunck1Length = 2;
            const expectedChunck1Array = [
                [
                    '0000',
                    '1000',
                    '2000',
                    '3000',
                    '4000',
                    '5000',
                    '6000',
                    '7000',
                    '8000',
                    '9000',
                    '1000',
                    '1100',
                    '1200',
                    '1300',
                    '1400',
                    '1500',
                    '1600',
                    '1700',
                    '1800',
                    '1900',
                    '2000',
                    '2100',
                    '2200',
                    '2300',
                    '2400',
                    '2500',
                    '2600',
                    '2700',
                    '2800',
                    '2900',
                    '3000',
                    '3100'
                ],
                [
                    '3200',
                    '3300',
                    '3400',
                    '3500',
                    '3600',
                    '3700',
                    '3800',
                    '3900',
                    '4000',
                    '4100',
                    '4200',
                    '4300',
                    '4400',
                    '4500',
                    '4600',
                    '4700',
                    '4800',
                    '4900',
                    '5000',
                    '5100',
                    '5200',
                    '5300',
                    '5400',
                    '5500',
                    '5600',
                    '5700',
                    '5800',
                    '5900',
                    '6000',
                    '6100',
                    '6200',
                    '6300'
                ]
            ];

            // // Act

            let nameCharsItemsSample = new Array<string>();
            for (let nbr = 0; nbr < 64; nbr++) {
                nameCharsItemsSample.push(BufferUtility.decPadStringEnd(nbr.toString(), byteToLengthSample, '0'));
            }

            // The _.chunk function creates an array of elements split into groups the length of the specified size.
            let chunck1 = BufferUtility.getChunkOfStringArray(nameCharsItemsSample, byteMaximumNumberOfNamesSample);

            // Assert
            expect(expectedChunck1Length).toEqual(chunck1.length);
            expect(expectedChunck1_1_30).toEqual(chunck1[1][30]);
            expect(expectedChunck1Array).toEqual(chunck1);
        });

        it('Should chunk an array of string with 64 items, Max items per group is 10 and Start Padding with 0', () => {
            // // Arrange

            const byteToLengthSample = 4;
            const byteMaximumNumberOfNamesSample = 32;

            const expectedChunck2_3_2 = '0032';
            const expectedChunck2Length = 7;
            const expectedChunck2Array = [
                ['0000', '0001', '0002', '0003', '0004', '0005', '0006', '0007', '0008', '0009'],
                ['0010', '0011', '0012', '0013', '0014', '0015', '0016', '0017', '0018', '0019'],
                ['0020', '0021', '0022', '0023', '0024', '0025', '0026', '0027', '0028', '0029'],
                ['0030', '0031', '0032', '0033', '0034', '0035', '0036', '0037', '0038', '0039'],
                ['0040', '0041', '0042', '0043', '0044', '0045', '0046', '0047', '0048', '0049'],
                ['0050', '0051', '0052', '0053', '0054', '0055', '0056', '0057', '0058', '0059'],
                ['0060', '0061', '0062', '0063']
            ];

            // // Act
            // Padding
            let nameCharsItemsSample = new Array<string>();
            for (let nbr = 0; nbr < 64; nbr++) {
                nameCharsItemsSample.push(BufferUtility.decPadStringStart(nbr.toString(), byteToLengthSample, '0'));
            }

            // The _.chunk function creates an array of elements split into groups the length of the specified size.
            let chunck2 = BufferUtility.getChunkOfStringArray(nameCharsItemsSample, 10);

            // Assert
            expect(expectedChunck2Length).toEqual(chunck2.length);
            expect(expectedChunck2_3_2).toEqual(chunck2[3][2]);
            expect(expectedChunck2Array).toEqual(chunck2);
        });
    });
});
