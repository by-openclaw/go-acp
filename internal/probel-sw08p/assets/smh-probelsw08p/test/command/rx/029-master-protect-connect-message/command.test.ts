import { MasterProtectConnectMessageCommand } from '../../../../src/command/rx/029-master-protect-connect-message/command';
import { MasterProtectConnectMessageCommandParams } from '../../../../src/command/rx/029-master-protect-connect-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('Master Protect Connect Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: MasterProtectConnectMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                deviceId: 908
            };

            // Act
            const command = new MasterProtectConnectMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: MasterProtectConnectMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                deviceId: -1
            };

            // Act
            const fct = () => new MasterProtectConnectMessageCommand(params /*, options*/);

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
            const params: MasterProtectConnectMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                deviceId: 1024
            };

            // Act
            const fct = () => new MasterProtectConnectMessageCommand(params);

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
        describe('General Master Protect Connect  Message CMD_029_0X1d', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.MASTER_PROTECT_CONNECT_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: MasterProtectConnectMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    deviceId: 907
                };

                // Act
                const command = new MasterProtectConnectMessageCommand(params /*, options*/);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '1D 00 00 00 00 03 8B', // data
                    7, // bytesCount
                    0x4e, // checksum
                    '10 02 1D 00 00 00 00 03 8B 07 4E 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: MasterProtectConnectMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    destinationId: 0,
                    deviceId: 907
                };

                // Act
                const command = new MasterProtectConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '1D 10 00 00 00 03 8B', // data
                    7, // bytesCount
                    0x3e, // checksum
                    '10 02 1D 10 10 00 00 00 03 8B 07 3E 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: MasterProtectConnectMessageCommandParams = {
                    matrixId: 0,
                    levelId: 16,
                    destinationId: 0,
                    deviceId: 907
                };

                // Act
                const command = new MasterProtectConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '1D 00 10 00 00 03 8B', // data
                    7, // bytesCount
                    0x3e, // checksum
                    '10 02 1D 00 10 10 00 00 03 8B 07 3E 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: MasterProtectConnectMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896,
                    deviceId: 907
                };

                // Act
                const command = new MasterProtectConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '1D 00 00 03 80 03 8B', // data
                    7, // bytesCount
                    0xcb, // checksum
                    '10 02 1D 00 00 03 80 03 8B 07 CB 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.MASTER_PROTECT_CONNECT_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: MasterProtectConnectMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16,
                    deviceId: 907
                };

                // Act
                const command = new MasterProtectConnectMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '1D 10 10 00 10 03 8B', // data
                    7, // bytesCount
                    0x1e, // checksum
                    '10 02 1D 10 10 10 10 00 10 10 03 8B 07 1E 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: MasterProtectConnectMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                deviceId: 907
            };

            // Act
            const command = new MasterProtectConnectMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
