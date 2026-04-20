import { ProtectDeviceNameResponseCommand } from '../../../../src/command/tx/018-protect-device-name-response-message/command';
import { ProtectDeviceNameResponseCommandParams } from '../../../../src/command/tx/018-protect-device-name-response-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('Protect Device Name Response Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: ProtectDeviceNameResponseCommandParams = {
                deviceId: 1,
                deviceName: '123'
            };

            // Act
            const command = new ProtectDeviceNameResponseCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: ProtectDeviceNameResponseCommandParams = {
                deviceId: -1,
                deviceName: ''
            };

            // Act
            const fct = () => new ProtectDeviceNameResponseCommand(params);

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
                expect(localeDataError.validationErrors?.deviceName.id).toBe(
                    CommandErrorsKeys.DEVICE_NAME_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: ProtectDeviceNameResponseCommandParams = {
                deviceId: 1024,
                deviceName: ''
            };
            // Act
            const fct = () => new ProtectDeviceNameResponseCommand(params);

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
                expect(localeDataError.validationErrors?.deviceName.id).toBe(
                    CommandErrorsKeys.DEVICE_NAME_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('Protect Device Name Response Message CMD_018_0X12', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.PROTECT_DEVICE_NAME_RESPONSE_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: ProtectDeviceNameResponseCommandParams = {
                    deviceId: 907,
                    deviceName: 'SMH-0907'
                };
                // Act
                const command = new ProtectDeviceNameResponseCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '12 07 0b 53 4d 48 2d 30 39 30 37', // data
                    11, // bytesCount
                    0xec, // checksum
                    '10 02 12 07 0b 53 4d 48 2d 30 39 30 37 0b ec 10 03' // buffer
                );
            });
            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: ProtectDeviceNameResponseCommandParams = {
                    deviceId: 0,
                    deviceName: 'SMH-0001'
                };
                // Act
                const command = new ProtectDeviceNameResponseCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '12 00 00 53 4d 48 2d 30 30 30 31', // data
                    11, // bytesCount
                    0x0d, // checksum
                    '10 02 12 00 00 53 4d 48 2d 30 30 30 31 0b 0d 10 03' // buffer
                );
            });
            it('Should create & pack the general command with padding (...)', () => {
                // Arrange
                const params: ProtectDeviceNameResponseCommandParams = {
                    deviceId: 15,
                    deviceName: 'SMH'
                };
                // Act
                const command = new ProtectDeviceNameResponseCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '12 00 0f 20 20 20 20 20 53 4d 48', // data
                    11, // bytesCount
                    0x4c, // checksum
                    '10 02 12 00 0f 20 20 20 20 20 53 4d 48 0b 4c 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.PROTECT_DEVICE_NAME_RESPONSE_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: ProtectDeviceNameResponseCommandParams = {
                    deviceId: 16,
                    deviceName: 'SMH-0016'
                };
                // Act
                const command = new ProtectDeviceNameResponseCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '12 00 10 53 4d 48 2d 30 30 31 36', // data
                    11, // bytesCount
                    0xf7, // checksum
                    '10 02 12 00 10 10 53 4d 48 2d 30 30 31 36 0b f7 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: ProtectDeviceNameResponseCommandParams = {
                deviceId: 0,
                deviceName: 'SMH-PNL1'
            };
            // Act
            const command = new ProtectDeviceNameResponseCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
