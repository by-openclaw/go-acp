import * as _ from 'lodash';
import { CrossPointTallyDumpWordCommand } from '../../../../src/command/tx/023-crosspoint-tally-dump-word-message/command';
import { CrossPointTallyDumpWordCommandParams } from '../../../../src/command/tx/023-crosspoint-tally-dump-word-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Tally Dump (Word) Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                // Add sourceItems
                buildDataArray.push(itemIndex);
            }

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 63,
                numberOfTalliesReturned: 64,
                sourceIdItems: buildDataArray
            };
            // Act
            const metaCommand = new CrossPointTallyDumpWordCommand(params);

            // Assert
            expect(metaCommand).toBeDefined();
            expect(metaCommand.params).toBe(params);
            expect(metaCommand.identifier).toBe(CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE);
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                // Add sourceItems
                buildDataArray.push(-1);
            }

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: -1,
                levelId: -1,
                firstDestinationId: -1,
                numberOfTalliesReturned: 0,
                sourceIdItems: buildDataArray
            };
            // Act
            const fct = () => new CrossPointTallyDumpWordCommand(params);

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
                expect(localeDataError.validationErrors?.numberOfTalliesReturned.id).toBe(
                    CommandErrorsKeys.NUMBER_OF_TALLIES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceIdAndMaximumNumberOfSources).toBeDefined();
                expect(localeDataError.validationErrors?.sourceIdAndMaximumNumberOfSources.id).toBe(
                    CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
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
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 400; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add sourceItems
                    buildDataArray.push(65536);
                }
            }

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: 256,
                levelId: 256,
                firstDestinationId: 65536,
                numberOfTalliesReturned: 65,
                sourceIdItems: buildDataArray
            };
            // Act
            const fct = () => new CrossPointTallyDumpWordCommand(params);

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
                expect(localeDataError.validationErrors?.numberOfTalliesReturned.id).toBe(
                    CommandErrorsKeys.NUMBER_OF_TALLIES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceIdAndMaximumNumberOfSources).toBeDefined();
                expect(localeDataError.validationErrors?.sourceIdAndMaximumNumberOfSources.id).toBe(
                    CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
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
        describe('General CrossPoint Tally Dump (Word) Message CMD_023_0X17', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                // generate an array of sourceItems
                const buildDataArray = new Array<number>();
                for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                    // Add sourceItems
                    buildDataArray.push(itemIndex);
                }

                const params: CrossPointTallyDumpWordCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstDestinationId: 0,
                    numberOfTalliesReturned: 64,
                    sourceIdItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointTallyDumpWordCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '17 00 40 00 00 00 00 00 01 00 02 00 03 00 04 00 05 00 06 00 07 00 08 00 09 00 0a 00 0b 00 0c 00 0d 00 0e 00 0f 00 10 00 11 00 12 00 13 00 14 00 15 00 16 00 17 00 18 00 19 00 1a 00 1b 00 1c 00 1d 00 1e 00 1f 00 20 00 21 00 22 00 23 00 24 00 25 00 26 00 27 00 28 00 29 00 2a 00 2b 00 2c 00 2d 00 2e 00 2f 00 30 00 31 00 32 00 33 00 34 00 35 00 36 00 37 00 38 00 39 00 3a 00 3b 00 3c 00 3d 00 3e 00 3f',
                        bytesCount: 0x85,
                        checksum: 0x44,
                        buffer:
                            '10 02 17 00 40 00 00 00 00 00 01 00 02 00 03 00 04 00 05 00 06 00 07 00 08 00 09 00 0a 00 0b 00 0c 00 0d 00 0e 00 0f 00 10 10 00 11 00 12 00 13 00 14 00 15 00 16 00 17 00 18 00 19 00 1a 00 1b 00 1c 00 1d 00 1e 00 1f 00 20 00 21 00 22 00 23 00 24 00 25 00 26 00 27 00 28 00 29 00 2a 00 2b 00 2c 00 2d 00 2e 00 2f 00 30 00 31 00 32 00 33 00 34 00 35 00 36 00 37 00 38 00 39 00 3a 00 3b 00 3c 00 3d 00 3e 00 3f 85 44 10 03'
                    },
                    {
                        data:
                            '17 00 40 00 40 00 40 00 41 00 42 00 43 00 44 00 45 00 46 00 47 00 48 00 49 00 4a 00 4b 00 4c 00 4d 00 4e 00 4f 00 50 00 51 00 52 00 53 00 54 00 55 00 56 00 57 00 58 00 59 00 5a 00 5b 00 5c 00 5d 00 5e 00 5f 00 60 00 61 00 62 00 63 00 64 00 65 00 66 00 67 00 68 00 69 00 6a 00 6b 00 6c 00 6d 00 6e 00 6f 00 70 00 71 00 72 00 73 00 74 00 75 00 76 00 77 00 78 00 79 00 7a 00 7b 00 7c 00 7d 00 7e 00 7f',
                        bytesCount: 0x85,
                        checksum: 0x04,
                        buffer:
                            '10 02 17 00 40 00 40 00 40 00 41 00 42 00 43 00 44 00 45 00 46 00 47 00 48 00 49 00 4a 00 4b 00 4c 00 4d 00 4e 00 4f 00 50 00 51 00 52 00 53 00 54 00 55 00 56 00 57 00 58 00 59 00 5a 00 5b 00 5c 00 5d 00 5e 00 5f 00 60 00 61 00 62 00 63 00 64 00 65 00 66 00 67 00 68 00 69 00 6a 00 6b 00 6c 00 6d 00 6e 00 6f 00 70 00 71 00 72 00 73 00 74 00 75 00 76 00 77 00 78 00 79 00 7a 00 7b 00 7c 00 7d 00 7e 00 7f 85 04 10 03'
                    }
                ]);
            });

            it('Should create & pack the general 1 command (...)', () => {
                // Arrange
                // generate an array of sourceItems
                const buildDataArray = new Array<number>();
                for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                    // Add sourceItems
                    buildDataArray.push(itemIndex);
                }

                const params: CrossPointTallyDumpWordCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstDestinationId: 890,
                    numberOfTalliesReturned: 64,
                    sourceIdItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointTallyDumpWordCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data: '17 00 40 03 7a 00 00 00 01 00 02 00 03 00 04 00 05',
                        bytesCount: 0x11,
                        checksum: 0x0c,
                        buffer: '10 02 17 00 40 03 7a 00 00 00 01 00 02 00 03 00 04 00 05 11 0c 10 03'
                    },
                    {
                        data:
                            '97 00 00 40 03 80 00 06 00 07 00 08 00 09 00 0a 00 0b 00 0c 00 0d 00 0e 00 0f 00 10 00 11 00 12 00 13 00 14 00 15 00 16 00 17 00 18 00 19 00 1a 00 1b 00 1c 00 1d 00 1e 00 1f 00 20 00 21 00 22 00 23 00 24 00 25 00 26 00 27 00 28 00 29 00 2a 00 2b 00 2c 00 2d 00 2e 00 2f 00 30 00 31 00 32 00 33 00 34 00 35 00 36 00 37 00 38 00 39 00 3a 00 3b 00 3c 00 3d 00 3e 00 3f 00 40 00 41 00 42 00 43 00 44 00 45',
                        bytesCount: 0x85,
                        checksum: 0xc0,
                        buffer:
                            '10 02 97 00 00 40 03 80 00 06 00 07 00 08 00 09 00 0a 00 0b 00 0c 00 0d 00 0e 00 0f 00 10 10 00 11 00 12 00 13 00 14 00 15 00 16 00 17 00 18 00 19 00 1a 00 1b 00 1c 00 1d 00 1e 00 1f 00 20 00 21 00 22 00 23 00 24 00 25 00 26 00 27 00 28 00 29 00 2a 00 2b 00 2c 00 2d 00 2e 00 2f 00 30 00 31 00 32 00 33 00 34 00 35 00 36 00 37 00 38 00 39 00 3a 00 3b 00 3c 00 3d 00 3e 00 3f 00 40 00 41 00 42 00 43 00 44 00 45 86 c0 10 03'
                    },
                    {
                        data:
                            '97 00 00 40 03 c0 00 46 00 47 00 48 00 49 00 4a 00 4b 00 4c 00 4d 00 4e 00 4f 00 50 00 51 00 52 00 53 00 54 00 55 00 56 00 57 00 58 00 59 00 5a 00 5b 00 5c 00 5d 00 5e 00 5f 00 60 00 61 00 62 00 63 00 64 00 65 00 66 00 67 00 68 00 69 00 6a 00 6b 00 6c 00 6d 00 6e 00 6f 00 70 00 71 00 72 00 73 00 74 00 75 00 76 00 77 00 78 00 79 00 7a 00 7b 00 7c 00 7d 00 7e 00 7f',
                        bytesCount: 0x7a,
                        checksum: 0x9b,
                        buffer:
                            '10 02 97 00 00 40 03 c0 00 46 00 47 00 48 00 49 00 4a 00 4b 00 4c 00 4d 00 4e 00 4f 00 50 00 51 00 52 00 53 00 54 00 55 00 56 00 57 00 58 00 59 00 5a 00 5b 00 5c 00 5d 00 5e 00 5f 00 60 00 61 00 62 00 63 00 64 00 65 00 66 00 67 00 68 00 69 00 6a 00 6b 00 6c 00 6d 00 6e 00 6f 00 70 00 71 00 72 00 73 00 74 00 75 00 76 00 77 00 78 00 79 00 7a 00 7b 00 7c 00 7d 00 7e 00 7f 85 04 10 03'
                    }
                ]);
            });
        });

        describe('Extended General CrossPoint Tally Dump (Word) Message CMD_151_0X97', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE;
            });
            it('Should create & pack the extended command (...)', () => {
                // Arrange
                // generate an array of sourceItems
                const buildDataArray = new Array<number>();
                for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                    // Add sourceItems
                    buildDataArray.push(itemIndex);
                }

                const params: CrossPointTallyDumpWordCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstDestinationId: 896,
                    numberOfTalliesReturned: 64,
                    sourceIdItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointTallyDumpWordCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '97 00 00 40 03 80 00 00 00 01 00 02 00 03 00 04 00 05 00 06 00 07 00 08 00 09 00 0a 00 0b 00 0c 00 0d 00 0e 00 0f 00 10 00 11 00 12 00 13 00 14 00 15 00 16 00 17 00 18 00 19 00 1a 00 1b 00 1c 00 1d 00 1e 00 1f 00 20 00 21 00 22 00 23 00 24 00 25 00 26 00 27 00 28 00 29 00 2a 00 2b 00 2c 00 2d 00 2e 00 2f 00 30 00 31 00 32 00 33 00 34 00 35 00 36 00 37 00 38 00 39 00 3a 00 3b 00 3c 00 3d 00 3e 00 3f',
                        bytesCount: 0x86,
                        checksum: 0x40,
                        buffer:
                            '10 02 97 00 00 40 03 80 00 00 00 01 00 02 00 03 00 04 00 05 00 06 00 07 00 08 00 09 00 0a 00 0b 00 0c 00 0d 00 0e 00 0f 00 10 10 00 11 00 12 00 13 00 14 00 15 00 16 00 17 00 18 00 19 00 1a 00 1b 00 1c 00 1d 00 1e 00 1f 00 20 00 21 00 22 00 23 00 24 00 25 00 26 00 27 00 28 00 29 00 2a 00 2b 00 2c 00 2d 00 2e 00 2f 00 30 00 31 00 32 00 33 00 34 00 35 00 36 00 37 00 38 00 39 00 3a 00 3b 00 3c 00 3d 00 3e 00 3f 86 40 10 03'
                    },
                    {
                        data:
                            '97 00 00 40 03 c0 00 40 00 41 00 42 00 43 00 44 00 45 00 46 00 47 00 48 00 49 00 4a 00 4b 00 4c 00 4d 00 4e 00 4f 00 50 00 51 00 52 00 53 00 54 00 55 00 56 00 57 00 58 00 59 00 5a 00 5b 00 5c 00 5d 00 5e 00 5f 00 60 00 61 00 62 00 63 00 64 00 65 00 66 00 67 00 68 00 69 00 6a 00 6b 00 6c 00 6d 00 6e 00 6f 00 70 00 71 00 72 00 73 00 74 00 75 00 76 00 77 00 78 00 79 00 7a 00 7b 00 7c 00 7d 00 7e 00 7f',
                        bytesCount: 0x86,
                        checksum: 0x00,
                        buffer:
                            '10 02 97 00 00 40 03 c0 00 40 00 41 00 42 00 43 00 44 00 45 00 46 00 47 00 48 00 49 00 4a 00 4b 00 4c 00 4d 00 4e 00 4f 00 50 00 51 00 52 00 53 00 54 00 55 00 56 00 57 00 58 00 59 00 5a 00 5b 00 5c 00 5d 00 5e 00 5f 00 60 00 61 00 62 00 63 00 64 00 65 00 66 00 67 00 68 00 69 00 6a 00 6b 00 6c 00 6d 00 6e 00 6f 00 70 00 71 00 72 00 73 00 74 00 75 00 76 00 77 00 78 00 79 00 7a 00 7b 00 7c 00 7d 00 7e 00 7f 86 00 10 03'
                    }
                ]);
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE;
            });
            it('Should create & pack the extended command (...)', () => {
                // Arrange
                // generate an array of sourceItems
                const buildDataArray = new Array<number>();
                for (let itemIndex = 0; itemIndex < 32; itemIndex++) {
                    // Add sourceItems
                    buildDataArray.push(itemIndex);
                }

                const params: CrossPointTallyDumpWordCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    firstDestinationId: 16,
                    numberOfTalliesReturned: 16,
                    sourceIdItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointTallyDumpWordCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '97 10 10 10 00 10 00 00 00 01 00 02 00 03 00 04 00 05 00 06 00 07 00 08 00 09 00 0a 00 0b 00 0c 00 0d 00 0e 00 0f',
                        bytesCount: 0x86,
                        checksum: 0x40,
                        buffer:
                            '10 02 97 10 10 10 10 10 10 00 10 10 00 00 00 01 00 02 00 03 00 04 00 05 00 06 00 07 00 08 00 09 00 0a 00 0b 00 0c 00 0d 00 0e 00 0f 26 8b 10 03'
                    },
                    {
                        data:
                            '97 10 10 10 00 20 00 10 00 11 00 12 00 13 00 14 00 15 00 16 00 17 00 18 00 19 00 1a 00 1b 00 1c 00 1d 00 1e 00 1f',
                        bytesCount: 0x86,
                        checksum: 0x00,
                        buffer:
                            '10 02 97 10 10 10 10 10 10 00 20 00 10 10 00 11 00 12 00 13 00 14 00 15 00 16 00 17 00 18 00 19 00 1a 00 1b 00 1c 00 1d 00 1e 00 1f 26 7b 10 03'
                    }
                ]);
            });
        });
    });

    describe('to Log Description of the general command', () => {
        it('Should log General command description', () => {
            // Arrange
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                // Add sourceItems
                buildDataArray.push(itemIndex);
            }

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 0,
                numberOfTalliesReturned: 64,
                sourceIdItems: buildDataArray
            };
            // Act
            const metaCommand = new CrossPointTallyDumpWordCommand(params);
            const description = metaCommand.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log Extended general command description', () => {
            // Arrange
            // generate an array of sourceItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 128; itemIndex++) {
                // Add sourceItems
                buildDataArray.push(itemIndex);
            }

            const params: CrossPointTallyDumpWordCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 896,
                numberOfTalliesReturned: 64,
                sourceIdItems: buildDataArray
            };
            // Act
            const metaCommand = new CrossPointTallyDumpWordCommand(params);
            const description = metaCommand.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
