import { CrossPointConnectMessageCommand } from '../../../../src/command/rx/002-crosspoint-connect-message/command';
import { CrossPointConnectMessageCommandParams } from '../../../../src/command/rx/002-crosspoint-connect-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Connect Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: CrossPointConnectMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 0
            };

            // Act
            const command = new CrossPointConnectMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: CrossPointConnectMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                sourceId: -1
            };

            // Act
            const fct = () => new CrossPointConnectMessageCommand(params);

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
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );

                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: CrossPointConnectMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                sourceId: 65536
            };

            // Act
            const fct = () => new CrossPointConnectMessageCommand(params);

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
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );

                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General CrossPoint Connect Message CMD_002_0X02', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_MESSAGE;
            });

            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 0 - sourceId = 0)', () => {
                // Arrange
                const params: CrossPointConnectMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    sourceId: 0
                };

                // Act
                const command = new CrossPointConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '02 00 00 00 00', // data
                    5, // bytesCount
                    0xf9, // checksum
                    '10 02 02 00 00 00 00 05 F9 10 03' // buffer
                );
            });

            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 895 - sourceId = 1023)', () => {
                // Arrange
                const params: CrossPointConnectMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 895,
                    sourceId: 1023
                };

                // Act
                const command = new CrossPointConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '02 00 67 7F 7F', // data
                    5, // bytesCount
                    0x94, // checksum
                    '10 02 02 00 67 7F 7F 05 94 10 03' // buffer
                );
            });

            it('Should create & pack the general command (matrixId = 15 - levelId = 15 - destinationId = 895 - sourceId = 1023)', () => {
                // Arrange
                const params: CrossPointConnectMessageCommandParams = {
                    matrixId: 15, // 4 bits coded
                    levelId: 15, // 4 bits codeded
                    destinationId: 895, // Multiplier 3 bits coded (896 DIV 128 = 7)
                    sourceId: 1023
                };

                // Act
                const command = new CrossPointConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '02 FF 67 7F 7F', // data
                    5, // bytesCount
                    0x95, // checksum
                    '10 02 02 FF 67 7F 7F 05 95 10 03' // buffer
                );
            });
        });

        describe('Extended General CrossPoint Connect Message CMD_130_0X82', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.CROSSPOINT_CONNECT_MESSAGE;
            });
            it('Should create & pack the extended command (matrixId = 16 - levelId = 15 - destinationId = 895 - sourceId = 1023) matrixId > 15 & matrixId = [DLE]', () => {
                // Arrange
                const params: CrossPointConnectMessageCommandParams = {
                    matrixId: 16,
                    levelId: 15,
                    destinationId: 895,
                    sourceId: 1023
                };

                // Act
                const command = new CrossPointConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '82 10 0F 03 7F 03 FF', // data
                    7, // bytesCount
                    0xd4, // checksum
                    '10 02 82 10 10 0F 03 7F 03 FF 07 D4 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 15 - levelId = 16 - destinationId = 895 - sourceId = 1023) levelId > 15 & levelId = [DLE]', () => {
                // Arrange
                const params: CrossPointConnectMessageCommandParams = {
                    matrixId: 15,
                    levelId: 16,
                    destinationId: 895,
                    sourceId: 1023
                };

                // Act
                const command = new CrossPointConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '82 0F 10 03 7F 03 FF', // data
                    7, // bytesCount
                    0xd4, // checksum
                    '10 02 82 0F 10 10 03 7F 03 FF 07 D4 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 896 - sourceId = 1023) destinationId > 895', () => {
                // Arrange
                const params: CrossPointConnectMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896,
                    sourceId: 1023
                };

                // Act
                const command = new CrossPointConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '82 00 00 03 80 03 FF', // data
                    7, // bytesCount
                    0xf2, // checksum
                    '10 02 82 00 00 03 80 03 FF 07 F2 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 895 - sourceId = 1024) sourceId > 1023', () => {
                // Arrange
                const params: CrossPointConnectMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 895,
                    sourceId: 1024
                };

                // Act
                const command = new CrossPointConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '82 00 00 03 7F 04 00', // data
                    7, // bytesCount
                    0xf1, // checksum
                    '10 02 82 00 00 03 7F 04 00 07 F1 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 255 - levelId = 255 - destinationId = 65534 - sourceId = 65534) [MAX Values] ', () => {
                // Arrange
                const params: CrossPointConnectMessageCommandParams = {
                    matrixId: 255,
                    levelId: 255,
                    destinationId: 65534,
                    sourceId: 65534
                };

                // Act
                const command = new CrossPointConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '82 FF FF FF FE FF FE', // data
                    7, // bytesCount
                    0x7f, // checksum
                    '10 02 82 FF FF FF FE FF FE 07 7F 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.CROSSPOINT_CONNECT_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16 - sourceId = 16))', () => {
                // Arrange
                const params: CrossPointConnectMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16,
                    sourceId: 16
                };

                // Act
                const command = new CrossPointConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '82 10 10 00 10 00 10', // data
                    7, // bytesCount
                    0x37, // checksum
                    '10 02 82 10 10 10 10 00 10 10 00 10 10 07 37 10 03' // buffer
                );
            });
        });
    });
    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: CrossPointConnectMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 0
            };

            // Act
            const command = new CrossPointConnectMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
        
        it('Should log Extended general command description', () => {
            // Arrange
            const params: CrossPointConnectMessageCommandParams = {
                matrixId: 16,
                levelId: 16,
                destinationId: 896,
                sourceId: 1024
            };

            // Act
            const command = new CrossPointConnectMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
