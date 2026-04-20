import * as _ from 'lodash';
import { CrossPointTallyDumpByteCommand } from '../../../../src/command/tx/022-crosspoint-tally-dump-byte-message/command';
import { CrossPointTallyDumpByteCommandParams } from '../../../../src/command/tx/022-crosspoint-tally-dump-byte-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Tally Dump (byte) Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the metaCommand with valid params, options', () => {
            // Arrange
            // generate an array of sourceIdItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 191; itemIndex++) {
                // Add the sourceId Items buffer to the array
                buildDataArray.push(itemIndex);
            }

            const params: CrossPointTallyDumpByteCommandParams = {
                matrixId: 0,
                levelId: 0,
                numberOfTalliesReturned: buildDataArray.length,
                firstDestinationId: 0,
                sourceIdItems: buildDataArray
            };
            // Act
            const metaCommand = new CrossPointTallyDumpByteCommand(params);

            // Assert
            expect(metaCommand).toBeDefined();
            expect(metaCommand.params).toBe(params);
            expect(metaCommand.identifier).toBe(CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_DUMP_BYTE_MESSAGE);
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            // generate an array of sourceIdItems
            const buildDataArray = new Array<number>();
              for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                // Add sourceItems
                buildDataArray.push(-1);
            }

            const params: CrossPointTallyDumpByteCommandParams = {
                matrixId: -1,
                levelId: -1,
                numberOfTalliesReturned: 0,
                firstDestinationId: -1,
                sourceIdItems: buildDataArray
            };

            // Act
            const fct = () => new CrossPointTallyDumpByteCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.talliesReturned.id).toBe(
                    CommandErrorsKeys.NUMBER_OF_TALLIES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceIdItems.id).toBe(
                    CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            // generate an array of sourceIdItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 257; itemIndex++) {
                // Add the sourceId Items buffer to the array
                buildDataArray.push(itemIndex);
            }
            const params: CrossPointTallyDumpByteCommandParams = {
                matrixId: 256,
                levelId: 256,
                numberOfTalliesReturned: buildDataArray.length,
                firstDestinationId: 256,
                sourceIdItems: buildDataArray
            };
            // Act
            const fct = () => new CrossPointTallyDumpByteCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.talliesReturned.id).toBe(
                    CommandErrorsKeys.NUMBER_OF_TALLIES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceIdItems.id).toBe(
                    CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General CrossPoint Tally Dump (byte) Message CMD_022_0X16', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_DUMP_BYTE_MESSAGE;
            });

            it('Should create & pack the general metaCommand (...)', () => {
                // Arrange
                // generate an array of sourceIdItems
                const buildDataArray = new Array<number>();
                for (let itemIndex = 0; itemIndex < 191; itemIndex++) {
                    // Add the sourceId Items buffer to the array
                    buildDataArray.push(itemIndex);
                }

                const params: CrossPointTallyDumpByteCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    numberOfTalliesReturned: buildDataArray.length,
                    firstDestinationId: 0,
                    sourceIdItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointTallyDumpByteCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '16 00 bf 00 00 01 02 03 04 05 06 07 08 09 0a 0b 0c 0d 0e 0f 10 11 12 13 14 15 16 17 18 19 1a 1b 1c 1d 1e 1f 20 21 22 23 24 25 26 27 28 29 2a 2b 2c 2d 2e 2f 30 31 32 33 34 35 36 37 38 39 3a 3b 3c 3d 3e 3f 40 41 42 43 44 45 46 47 48 49 4a 4b 4c 4d 4e 4f 50 51 52 53 54 55 56 57 58 59 5a 5b 5c 5d 5e 5f 60 61 62 63 64 65 66 67 68 69 6a 6b 6c 6d 6e 6f 70 71 72 73 74 75 76 77 78 79 7a 7b 7c 7d 7e 7f 80 81 82 83 84 85 86 87 88 89 8a 8b 8c 8d 8e 8f 90 91 92 93 94 95 96 97 98 99 9a 9b 9c 9d 9e 9f a0 a1 a2 a3 a4 a5 a6 a7 a8 a9 aa ab ac ad ae af b0 b1 b2 b3 b4 b5 b6 b7 b8 b9 ba bb bc bd be', // data
                        bytesCount: 195, // bytesCount
                        checksum: 0x87, // checksum
                        buffer:
                            '10 02 16 00 bf 00 00 01 02 03 04 05 06 07 08 09 0a 0b 0c 0d 0e 0f 10 10 11 12 13 14 15 16 17 18 19 1a 1b 1c 1d 1e 1f 20 21 22 23 24 25 26 27 28 29 2a 2b 2c 2d 2e 2f 30 31 32 33 34 35 36 37 38 39 3a 3b 3c 3d 3e 3f 40 41 42 43 44 45 46 47 48 49 4a 4b 4c 4d 4e 4f 50 51 52 53 54 55 56 57 58 59 5a 5b 5c 5d 5e 5f 60 61 62 63 64 65 66 67 68 69 6a 6b 6c 6d 6e 6f 70 71 72 73 74 75 76 77 78 79 7a 7b 7c 7d 7e 7f 80 81 82 83 84 85 86 87 88 89 8a 8b 8c 8d 8e 8f 90 91 92 93 94 95 96 97 98 99 9a 9b 9c 9d 9e 9f a0 a1 a2 a3 a4 a5 a6 a7 a8 a9 aa ab ac ad ae af b0 b1 b2 b3 b4 b5 b6 b7 b8 b9 ba bb bc bd be c3 87 10 03' // buffer
                    }
                ]);
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a metaCommand', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a metaCommand
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_DUMP_BYTE_MESSAGE;
            });

            it('Should create & pack the general metaCommand (...)', () => {
                // Arrange
                // generate an array of sourceIdItems
                const buildDataArray = new Array<number>();
                for (let itemIndex = 0; itemIndex < 17; itemIndex++) {
                    // Add the sourceId Items buffer to the array
                    buildDataArray.push(itemIndex);
                }

                const params: CrossPointTallyDumpByteCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    numberOfTalliesReturned: buildDataArray.length,
                    firstDestinationId: 16,
                    sourceIdItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointTallyDumpByteCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data: '16 00 11 10 00 01 02 03 04 05 06 07 08 09 0a 0b 0c 0d 0e 0f 10', // data
                        bytesCount: 0x15, // bytesCount
                        checksum: 0x2c, // checksum
                        buffer: '10 02 16 00 11 10 10 00 01 02 03 04 05 06 07 08 09 0a 0b 0c 0d 0e 0f 10 10 15 2c 10 03' // buffer
                    }
                ]);
            });
        });
    });

    describe('to Log Description of the general metaCommand', () => {
        it('Should log General metaCommand description', () => {
            // Arrange
            // generate an array of sourceIdItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 17; itemIndex++) {
                // Add the sourceId Items buffer to the array
                buildDataArray.push(itemIndex);
            }

            const params: CrossPointTallyDumpByteCommandParams = {
                matrixId: 0,
                levelId: 0,
                numberOfTalliesReturned: buildDataArray.length,
                firstDestinationId: 16,
                sourceIdItems: buildDataArray
            };
            // Act
            const metaCommand = new CrossPointTallyDumpByteCommand(params);
            const description = metaCommand.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
