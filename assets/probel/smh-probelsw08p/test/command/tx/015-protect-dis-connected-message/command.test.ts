import { ProtectDiscConnectedCommand } from '../../../../src/command/tx/015-protect-dis-connected-message/command';
import { ProtectDiscConnectedCommandParams } from '../../../../src/command/tx/015-protect-dis-connected-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import {
    ProtectDiscConnectedCommandOptions,
    ProtectDetails
} from '../../../../src/command/tx/015-protect-dis-connected-message/options';

describe('Protect Dis-Connected Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: ProtectDiscConnectedCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                deviceId: 907
            };
            const options: ProtectDiscConnectedCommandOptions = {
                protectDetails: ProtectDetails.OEM_PROTECTED
            };

            // Act
            const command = new ProtectDiscConnectedCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: ProtectDiscConnectedCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                deviceId: -1
            };
            const options: ProtectDiscConnectedCommandOptions = {
                protectDetails: -1
            };

            // Act
            const fct = () => new ProtectDiscConnectedCommand(params, options);

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
            const params: ProtectDiscConnectedCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                deviceId: 1024
            };
            const options: ProtectDiscConnectedCommandOptions = {
                protectDetails: 27
            };
            // Act
            const fct = () => new ProtectDiscConnectedCommand(params, options);

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
        describe('Protect Dis-Connected Message CMD_015_0X0f', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.PROTECT_DIS_CONNECTED_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: ProtectDiscConnectedCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    deviceId: 907
                };
                const options: ProtectDiscConnectedCommandOptions = {
                    protectDetails: ProtectDetails.NOT_PROTECTED
                };
                // Act
                const command = new ProtectDiscConnectedCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '0f 00 00 07 00 0b', // data
                    6, // bytesCount
                    0xd9, // checksum
                    '10 02 0f 00 00 07 00 0b 06 d9 10 03' // buffer
                );
            });
        });

        describe('Extended Protect Dis-Connected Message CMD_143_0X8f', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.PROTECT_DIS_CONNECTED_MESSAGE;
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectDiscConnectedCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    destinationId: 0,
                    deviceId: 907
                };
                const options: ProtectDiscConnectedCommandOptions = {
                    protectDetails: ProtectDetails.NOT_PROTECTED
                };
                // Act
                const command = new ProtectDiscConnectedCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8f 10 00 00 00 00 03 8b', // data
                    8, // bytesCount
                    0xcb, // checksum
                    '10 02 8f 10 10 00 00 00 00 03 8b 08 cb 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectDiscConnectedCommandParams = {
                    matrixId: 0,
                    levelId: 16,
                    destinationId: 0,
                    deviceId: 907
                };
                const options: ProtectDiscConnectedCommandOptions = {
                    protectDetails: ProtectDetails.NOT_PROTECTED
                };
                // Act
                const command = new ProtectDiscConnectedCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8f 00 10 00 00 00 03 8b', // data
                    8, // bytesCount
                    0xcb, // checksum
                    '10 02 8f 00 10 10 00 00 00 03 8b 08 cb 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectDiscConnectedCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896,
                    deviceId: 907
                };
                const options: ProtectDiscConnectedCommandOptions = {
                    protectDetails: ProtectDetails.NOT_PROTECTED
                };
                // Act
                const command = new ProtectDiscConnectedCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8f 00 00 00 03 80 03 8b', // data
                    8, // bytesCount
                    0x58, // checksum
                    '10 02 8f 00 00 00 03 80 03 8b 08 58 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.PROTECT_DIS_CONNECTED_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: ProtectDiscConnectedCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16,
                    deviceId: 16
                };
                const options: ProtectDiscConnectedCommandOptions = {
                    protectDetails: ProtectDetails.NOT_PROTECTED
                };
                // Act
                const command = new ProtectDiscConnectedCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8f 10 10 00 00 10 00 10', // data
                    8, // bytesCount
                    0x29, // checksum
                    '10 02 8f 10 10 10 10 00 00 10 10 00 10 10 08 29 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: ProtectDiscConnectedCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                deviceId: 907
            };
            const options: ProtectDiscConnectedCommandOptions = {
                protectDetails: ProtectDetails.OEM_PROTECTED
            };
            // Act
            const command = new ProtectDiscConnectedCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log Extended general command description', () => {
            // Arrange
            const params: ProtectDiscConnectedCommandParams = {
                matrixId: 255,
                levelId: 255,
                destinationId: 65535,
                deviceId: 907
            };
            const options: ProtectDiscConnectedCommandOptions = {
                protectDetails: ProtectDetails.PRO_BEL_PROTECTED
            };
            // Act
            const command = new ProtectDiscConnectedCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
