import { CrossPointInterrogateMessageCommand } from '../../../../src/command/rx/001-crosspoint-interrogate-message/command';
import { CrossPointInterrogateMessageCommandParams } from '../../../../src/command/rx/001-crosspoint-interrogate-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Interrogate Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: CrossPointInterrogateMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0
            };

            // Act
            const command = new CrossPointInterrogateMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: CrossPointInterrogateMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1
            };

            // Act
            const fct = () => new CrossPointInterrogateMessageCommand(params);

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
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: CrossPointInterrogateMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536
            };

            // Act
            const fct = () => new CrossPointInterrogateMessageCommand(params);

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
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Crosspoint Interrogate Message CMD_001_0X01', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_INTERROGATE_MESSAGE;
            });

            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 0)', () => {
                // Arrange
                const params: CrossPointInterrogateMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0
                };

                // Act
                const command = new CrossPointInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '01 00 00 00', // data
                    4, // bytesCount
                    0xfb, // checksum
                    '10 02 01 00 00 00 04 FB 10 03' // buffer
                );
            });

            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 895)', () => {
                // Arrange
                const params: CrossPointInterrogateMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 895
                };

                // Act
                const command = new CrossPointInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '01 00 60 7F', // data
                    4, // bytesCount
                    0x1c, // checksum
                    '10 02 01 00 60 7F 04 1C 10 03' // buffer
                );
            });

            it('Should create & pack the general command (matrixId = 15 - levelId = 15 - destinationId = 895)', () => {
                // Arrange
                const params: CrossPointInterrogateMessageCommandParams = {
                    matrixId: 15, // 4 bits coded
                    levelId: 15, // 4 bits codeded
                    destinationId: 895 // Multiplier 3 bits coded (896 DIV 128 = 7)
                };

                // Act
                const command = new CrossPointInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '01 FF 60 7F', // data
                    4, // bytesCount
                    0x1d, // checksum
                    '10 02 01 FF 60 7F 04 1D 10 03' // buffer
                );
            });
        });

        describe('Extended General Crosspoint Interrogate Message CMD_129_0X81', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.CROSSPOINT_INTERROGATE_MESSAGE;
            });
            it('Should create & pack the extended command (matrixId = 16 - levelId = 15 - destinationId = 895) matrixId > 15 & matrixId = [DLE]', () => {
                // Arrange
                const params: CrossPointInterrogateMessageCommandParams = {
                    matrixId: 16,
                    levelId: 15,
                    destinationId: 895
                };

                // Act
                const command = new CrossPointInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '81 10 0F 03 7F', // data
                    5, // bytesCount
                    0xd9, // checksum
                    '10 02 81 10 10 0F 03 7F 05 D9 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 15 - levelId = 16 - destinationId = 895) levelId > 15 & levelId = [DLE]', () => {
                // Arrange
                const params: CrossPointInterrogateMessageCommandParams = {
                    matrixId: 15,
                    levelId: 16,
                    destinationId: 895
                };

                // Act
                const command = new CrossPointInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '81 0F 10 03 7F', // data
                    5, // bytesCount
                    0xd9, // checksum
                    '10 02 81 0F 10 10 03 7F 05 D9 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 896) destinationId > 895', () => {
                // Arrange
                const params: CrossPointInterrogateMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896
                };

                // Act
                const command = new CrossPointInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '81 00 00 03 80', // data
                    5, // bytesCount
                    0xf7, // checksum
                    '10 02 81 00 00 03 80 05 F7 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 255 - levelId = 255 - destinationId = 65535) [MAX Values] ', () => {
                // Arrange
                const params: CrossPointInterrogateMessageCommandParams = {
                    matrixId: 255,
                    levelId: 255,
                    destinationId: 65535
                };

                // Act
                const command = new CrossPointInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '81 FF FF FF FF', // data
                    5, // bytesCount
                    0x7e, // checksum
                    '10 02 81 FF FF FF FF 05 7E 10 03' // buffer
                );
            });
        });
        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.CROSSPOINT_INTERROGATE_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: CrossPointInterrogateMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16
                };

                // Act
                const command = new CrossPointInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '81 10 10 00 10', // data
                    5, // bytesCount
                    0x4a, // checksum
                    '10 02 81 10 10 10 10 00 10 10 05 4A 10 03' // buffer
                );
            });
        });
    });
    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: CrossPointInterrogateMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0
            };

            // Act
            const command = new CrossPointInterrogateMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
        it('Should log Extended general command description', () => {
            // Arrange
            const params: CrossPointInterrogateMessageCommandParams = {
                matrixId: 16,
                levelId: 16,
                destinationId: 16
            };

            // Act
            const command = new CrossPointInterrogateMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
