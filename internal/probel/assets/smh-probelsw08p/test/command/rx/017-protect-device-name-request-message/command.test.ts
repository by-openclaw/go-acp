import { ProtectDeviceNameRequestMessageCommand } from '../../../../src/command/rx/017-protect-device-name-request-message/command';
import { ProtectDeviceNameRequestMessageCommandParams } from '../../../../src/command/rx/017-protect-device-name-request-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('Protect Device Name Request Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: ProtectDeviceNameRequestMessageCommandParams = {
                deviceId: 908
            };

            // Act
            const command = new ProtectDeviceNameRequestMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: ProtectDeviceNameRequestMessageCommandParams = {
                deviceId: -1
            };

            // Act
            const fct = () => new ProtectDeviceNameRequestMessageCommand(params /*, options*/);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.deviceId.id).toBe(
                    CommandErrorsKeys.DEVICE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: ProtectDeviceNameRequestMessageCommandParams = {
                deviceId: 1024
            };

            // Act
            const fct = () => new ProtectDeviceNameRequestMessageCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.deviceId.id).toBe(
                    CommandErrorsKeys.DEVICE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Protect Device Name Request Message CMD_017_0X11', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.PROTECT_DEVICE_NAME_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: ProtectDeviceNameRequestMessageCommandParams = {
                    deviceId: 907
                };

                // Act
                const command = new ProtectDeviceNameRequestMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '11 07 0B', // data
                    3, // bytesCount
                    0xda, // checksum
                    '10 02 11 07 0B 03 DA 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.PROTECT_DEVICE_NAME_REQUEST_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (deviceId=16))', () => {
                // Arrange
                const params: ProtectDeviceNameRequestMessageCommandParams = {
                    deviceId: 16
                };

                // Act
                const command = new ProtectDeviceNameRequestMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '11 00 10', // data
                    3, // bytesCount
                    0xdc, // checksum
                    '10 02 11 00 10 10 03 DC 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: ProtectDeviceNameRequestMessageCommandParams = {
                deviceId: 907
            };

            // Act
            const command = new ProtectDeviceNameRequestMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
