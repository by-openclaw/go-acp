import { ProtectTallyCommand } from '../../../../src/command/tx/011-protect-tally-message/command';
import { ProtectTallyCommandParams } from '../../../../src/command/tx/011-protect-tally-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { ProtectTallyCommandOptions, ProtectDetails } from '../../../../src/command/tx/011-protect-tally-message/option';

describe('Protect Tally Message Command)', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: ProtectTallyCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                deviceId: 908
            };
            const options: ProtectTallyCommandOptions = {
                protectDetails: ProtectDetails.OEM_PROTECTED
            };

            // Act
            const command = new ProtectTallyCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: ProtectTallyCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                deviceId: -1
            };
            const options: ProtectTallyCommandOptions = {
                protectDetails: -1
            };

            // Act
            const fct = () => new ProtectTallyCommand(params, options);

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
            const params: ProtectTallyCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                deviceId: 1024
            };
            const options: ProtectTallyCommandOptions = {
                protectDetails: 27
            };
            // Act
            const fct = () => new ProtectTallyCommand(params, options);

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
        describe('General Protect Tally Message CMD_011_0X0b', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.PROTECT_TALLY_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: ProtectTallyCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    deviceId: 0
                };
                const options: ProtectTallyCommandOptions = {
                    protectDetails: ProtectDetails.NOT_PROTECTED
                };
                // Act
                const command = new ProtectTallyCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '0B 00 00 00 00 00', // data
                    6, // bytesCount
                    0xef, // checksum
                    '10 02 0B 00 00 00 00 00 06 EF 10 03' // buffer
                );
            });
        });

        describe('Extended Protect Tally Message CMD_139_0X8b', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.PROTECT_TALLY_MESSAGE;
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectTallyCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    destinationId: 0,
                    deviceId: 0
                };
                const options: ProtectTallyCommandOptions = {
                    protectDetails: ProtectDetails.NOT_PROTECTED
                };
                // Act
                const command = new ProtectTallyCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8B 10 00 00 00 00 00 00', // data
                    8, // bytesCount
                    0x5d, // checksum
                    '10 02 8B 10 10 00 00 00 00 00 00 08 5D 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectTallyCommandParams = {
                    matrixId: 0,
                    levelId: 16,
                    destinationId: 0,
                    deviceId: 0
                };
                const options: ProtectTallyCommandOptions = {
                    protectDetails: ProtectDetails.NOT_PROTECTED
                };
                // Act
                const command = new ProtectTallyCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8B 00 10 00 00 00 00 00', // data
                    8, // bytesCount
                    0x5d, // checksum
                    '10 02 8B 00 10 10 00 00 00 00 00 08 5D 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: ProtectTallyCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896,
                    deviceId: 0
                };
                const options: ProtectTallyCommandOptions = {
                    protectDetails: ProtectDetails.NOT_PROTECTED
                };
                // Act
                const command = new ProtectTallyCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8b 00 00 00 03 80 00 00', // data
                    8, // bytesCount
                    0xea, // checksum
                    '10 02 8b 00 00 00 03 80 00 00 08 ea 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.PROTECT_TALLY_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: ProtectTallyCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16,
                    deviceId: 0
                };
                const options: ProtectTallyCommandOptions = {
                    protectDetails: ProtectDetails.NOT_PROTECTED
                };
                // Act
                const command = new ProtectTallyCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '8B 10 10 00 00 10 00 00', // data
                    8, // bytesCount
                    0x3d, // checksum
                    '10 02 8B 10 10 10 10 00 00 10 10 00 00 08 3D 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: ProtectTallyCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                deviceId: 0
            };
            const options: ProtectTallyCommandOptions = {
                protectDetails: ProtectDetails.NOT_PROTECTED
            };
            // Act
            const command = new ProtectTallyCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log Extended command description', () => {
            // Arrange
            const params: ProtectTallyCommandParams = {
                matrixId: 255,
                levelId: 255,
                destinationId: 65535,
                deviceId: 1023
            };
            const options: ProtectTallyCommandOptions = {
                protectDetails: ProtectDetails.NOT_PROTECTED
            };
            // Act
            const command = new ProtectTallyCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
