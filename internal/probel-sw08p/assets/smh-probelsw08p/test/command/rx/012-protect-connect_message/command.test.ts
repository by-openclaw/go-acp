import { ProtectConnectMessageCommand } from '../../../../src/command/rx/012-protect-connect-message/command';
import { ProtectConnectMessageCommandParams } from '../../../../src/command/rx/012-protect-connect-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('Protect Connect Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: ProtectConnectMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                deviceId: 908
            };

            // Act
            const command = new ProtectConnectMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: ProtectConnectMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                deviceId: -1
            };

            // Act
            const fct = () => new ProtectConnectMessageCommand(params /*, options*/);

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
            const params: ProtectConnectMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                deviceId: 1024
            };

            // Act
            const fct = () => new ProtectConnectMessageCommand(params);

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
        describe('General Protect Connect Message CMD_012_0X0c', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.PROTECT_CONNECT_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: ProtectConnectMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    deviceId: 907
                };

                // Act
                const command = new ProtectConnectMessageCommand(params /*, options*/);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '0C 00 07 00 0B', // data
                    5, // bytesCount
                    0xdd, // checksum
                    '10 02 0C 00 07 00 0B 05 DD 10 03' // buffer
                );
            });
        });

        describe('Extended General Protect Connect Message CMD_140_0X8c', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.PROTECT_CONNECT_MESSAGE;
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectConnectMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    destinationId: 0,
                    deviceId: 907
                };

                // Act
                const command = new ProtectConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8C 10 00 00 00 03 8B', // data
                    7, // bytesCount
                    0xcf, // checksum
                    '10 02 8C 10 10 00 00 00 03 8B 07 CF 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectConnectMessageCommandParams = {
                    matrixId: 0,
                    levelId: 16,
                    destinationId: 0,
                    deviceId: 907
                };

                // Act
                const command = new ProtectConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8C 00 10 00 00 03 8B', // data
                    7, // bytesCount
                    0xcf, // checksum
                    '10 02 8C 00 10 10 00 00 03 8B 07 CF 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectConnectMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896,
                    deviceId: 907
                };

                // Act
                const command = new ProtectConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8C 00 00 03 80 03 8B', // data
                    7, // bytesCount
                    0x5c, // checksum
                    '10 02 8C 00 00 03 80 03 8B 07 5C 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.PROTECT_CONNECT_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: ProtectConnectMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16,
                    deviceId: 907
                };

                // Act
                const command = new ProtectConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8C 10 10 00 10 03 8B', // data
                    7, // bytesCount
                    0xaf, // checksum
                    '10 02 8C 10 10 10 10 00 10 10 03 8B 07 AF 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: ProtectConnectMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                deviceId: 907
            };

            // Act
            const command = new ProtectConnectMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log Extended general command description', () => {
            // Arrange
            const params: ProtectConnectMessageCommandParams = {
                matrixId: 16,
                levelId: 16,
                destinationId: 897,
                deviceId: 907
            };
            /*
            const options: ProtectConnectMessageCommandOptions = {
                // Add properties
            };
*/
            // Act
            const command = new ProtectConnectMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
