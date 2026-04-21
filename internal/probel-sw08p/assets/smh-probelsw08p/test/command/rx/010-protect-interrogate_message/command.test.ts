import { ProtectInterrogateMessageCommand } from '../../../../src/command/rx/010-protect-interrogate-message/command';
import { ProtectInterrogateMessageCommandParams } from '../../../../src/command/rx/010-protect-interrogate-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('Protect Interrogate Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: ProtectInterrogateMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                deviceId: 908
            };

            // Act
            const command = new ProtectInterrogateMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: ProtectInterrogateMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                deviceId: -1
            };

            // Act
            const fct = () => new ProtectInterrogateMessageCommand(params);

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
                expect(localeDataError.validationErrors?.deviceId.id).toBe(
                    CommandErrorsKeys.DEVICE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: ProtectInterrogateMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                deviceId: 1024
            };

            // Act
            const fct = () => new ProtectInterrogateMessageCommand(params);

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
                expect(localeDataError.validationErrors?.deviceId.id).toBe(
                    CommandErrorsKeys.DEVICE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Protect Interrogate Message CMD_010_0X0A', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.PROTECT_INTERROGATE_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: ProtectInterrogateMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    deviceId: 0
                };

                // Act
                const command = new ProtectInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '0A 00 00 00', // data
                    4, // bytesCount
                    0xf2, // checksum
                    '10 02 0A 00 00 00 04 F2 10 03' // buffer
                );
            });
        });

        describe('Extended General Protect Interrogate Message CMD_138_0X8A', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.PROTECT_INTERROGATE_MESSAGE;
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectInterrogateMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    destinationId: 0,
                    deviceId: 0
                };

                // Act
                const command = new ProtectInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8A 10 00 00 00', // data
                    5, // bytesCount
                    0x61, // checksum
                    '10 02 8A 10 10 00 00 00 05 61 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectInterrogateMessageCommandParams = {
                    matrixId: 0,
                    levelId: 16,
                    destinationId: 0,
                    deviceId: 0
                };

                // Act
                const command = new ProtectInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8A 00 10 00 00', // data
                    5, // bytesCount
                    0x61, // checksum
                    '10 02 8A 00 10 10 00 00 05 61 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectInterrogateMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896,
                    deviceId: 0
                };

                // Act
                const command = new ProtectInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8A 00 00 03 80', // data
                    5, // bytesCount
                    0xee, // checksum
                    '10 02 8A 00 00 03 80 05 EE 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.PROTECT_INTERROGATE_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: ProtectInterrogateMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16,
                    deviceId: 0
                };

                // Act
                const command = new ProtectInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8A 10 10 00 10', // data
                    5, // bytesCount
                    0x41, // checksum
                    '10 02 8A 10 10 10 10 00 10 10 05 41 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log general command description', () => {
            // Arrange
            const params: ProtectInterrogateMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                deviceId: 0
            };

            // Act
            const command = new ProtectInterrogateMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log extended command description', () => {
            // Arrange
            const params: ProtectInterrogateMessageCommandParams = {
                matrixId: 255,
                levelId: 255,
                destinationId: 65535,
                deviceId: 1023
            };

            // Act
            const command = new ProtectInterrogateMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
