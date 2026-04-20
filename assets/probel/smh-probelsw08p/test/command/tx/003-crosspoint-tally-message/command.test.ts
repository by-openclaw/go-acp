import { CrossPointTallyMessageCommand } from '../../../../src/command/tx/003-crosspoint-tally-message/command';
import { CrossPointTallyMessageCommandParams } from '../../../../src/command/tx/003-crosspoint-tally-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Tally Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: CrossPointTallyMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 0,
                statusId: 0
            };

            // Act
            const command = new CrossPointTallyMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: CrossPointTallyMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                sourceId: -1,
                statusId: -1
            };

            // Act
            const fct = () => new CrossPointTallyMessageCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: CrossPointTallyMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                sourceId: 65536,
                statusId: 0
            };

            // Act
            const fct = () => new CrossPointTallyMessageCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );

                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General CrossPoint Tally Message CMD_003_0X03', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_MESSAGE;
            });

            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 0 - sourceId = 0 - statusId = 0)', () => {
                // Arrange
                const params: CrossPointTallyMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    sourceId: 0,
                    statusId: 0
                };

                // Act
                const command = new CrossPointTallyMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '03 00 00 00 00', // data
                    5, // bytesCount
                    0xf8, // checksum
                    '10 02 03 00 00 00 00 05 F8 10 03' // buffer
                );
            });
            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 895 - sourceId = 0 - statusId = doesnt matter)', () => {
                // Arrange
                const params: CrossPointTallyMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 895,
                    sourceId: 0,
                    statusId: 0
                };

                // Act
                const command = new CrossPointTallyMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '03 00 60 7F 00', // data
                    5, // bytesCount
                    0x19, // checksum
                    '10 02 03 00 60 7F 00 05 19 10 03' // buffer
                );
            });
            it('Should create & pack the general command (matrixId = 15 - levelId = 15 - destinationId = 895 - sourceId = 1023 - statusId = doesnt matter)', () => {
                // Arrange
                const params: CrossPointTallyMessageCommandParams = {
                    matrixId: 15, // 4 bits coded
                    levelId: 15, // 4 bits codeded
                    destinationId: 895, // Multiplier 3 bits coded (896 DIV 128 = 7)
                    sourceId: 1023,
                    statusId: 0
                };

                // Act
                const command = new CrossPointTallyMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '03 FF 67 7F 7F', // data
                    5, // bytesCount
                    0x94, // checksum
                    '10 02 03 FF 67 7F 7F 05 94 10 03' // buffer
                );
            });
        });

        describe('Extended General CrossPoint Tally Message CMD_131_0X83', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.CROSSPOINT_TALLY_MESSAGE;
            });
            it('Should create & pack the extended command (matrixId = 16 - levelId = 15 - destinationId = 895 - sourceId = 1023) matrixId > 15 & matrixId = [DLE]', () => {
                // Arrange
                const params: CrossPointTallyMessageCommandParams = {
                    matrixId: 16,
                    levelId: 15,
                    destinationId: 895,
                    sourceId: 1023,
                    statusId: 0
                };

                // Act
                const command = new CrossPointTallyMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '83 10 0F 03 7F 03 FF 00', // data
                    8, // bytesCount
                    0xd2, // checksum
                    '10 02 83 10 10 0F 03 7F 03 FF 00 08 D2 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 15 - levelId = 16 - destinationId = 895 - sourceId = 1023) levelId > 15 & levelId = [DLE]', () => {
                // Arrange
                const params: CrossPointTallyMessageCommandParams = {
                    matrixId: 15,
                    levelId: 16,
                    destinationId: 895,
                    sourceId: 1023,
                    statusId: 0
                };

                // Act
                const command = new CrossPointTallyMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '83 0F 10 03 7F 03 FF 00 ', // data
                    8, // bytesCount
                    0xd2, // checksum
                    '10 02 83 0F 10 10 03 7F 03 FF 00 08 D2 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 896 - sourceId = 1023) destinationId > 895', () => {
                // Arrange
                const params: CrossPointTallyMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896,
                    sourceId: 1023,
                    statusId: 0
                };

                // Act
                const command = new CrossPointTallyMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '83 00 00 03 80 03 FF 00', // data
                    8, // bytesCount
                    0xf0, // checksum
                    '10 02 83 00 00 03 80 03 FF 00 08 F0 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 0 - sourceId = 1024) sourceId > 1023', () => {
                // Arrange
                const params: CrossPointTallyMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    sourceId: 1024,
                    statusId: 0
                };

                // Act
                const command = new CrossPointTallyMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '83 00 00 00 00 04 00 00', // data
                    8, // bytesCount
                    0x71, // checksum
                    '10 02 83 00 00 00 00 04 00 00 08 71 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 255 - levelId = 255 - destinationId = 65534 - sourceId = 52428) [MAX Values] ', () => {
                // Arrange
                const params: CrossPointTallyMessageCommandParams = {
                    matrixId: 255,
                    levelId: 255,
                    destinationId: 65534,
                    // TODO: the emulator doesnt accept more than sourceId > 52428
                    sourceId: 52428,
                    statusId: 0
                };

                // Act
                const command = new CrossPointTallyMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '83 FF FF FF FE CC CC 00', // data
                    8, // bytesCount
                    0xe2, // checksum
                    '10 02 83 FF FF FF FE CC CC 00 08 E2 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.CROSSPOINT_TALLY_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: CrossPointTallyMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16,
                    sourceId: 16,
                    statusId: 0
                };

                // Act
                const command = new CrossPointTallyMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '83 10 10 00 10 00 10 00', // data
                    8, // bytesCount
                    0x35, // checksum
                    '10 02 83 10 10 10 10 00 10 10 00 10 10 00 08 35 10 03' // buffer
                );
            });
        });
    });
    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: CrossPointTallyMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 895,
                sourceId: 0,
                statusId: 0
            };

            // Act
            const command = new CrossPointTallyMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
        it('Should log Extended general command description', () => {
            // Arrange
            const params: CrossPointTallyMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 1024,
                statusId: 0
            };

            // Act
            const command = new CrossPointTallyMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
