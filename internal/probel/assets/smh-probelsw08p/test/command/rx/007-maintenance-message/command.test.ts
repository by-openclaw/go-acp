import { MaintenanceMessageCommand } from '../../../../src/command/rx/007-maintenance-message/command';
import { MaintenanceMessageCommandParams } from '../../../../src/command/rx/007-maintenance-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import {
    MaintenanceMessageCommandOptions,
    MaintenanceFunction
} from '../../../../src/command/rx/007-maintenance-message/options';

describe('Maintenance Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: MaintenanceMessageCommandParams = {
                matrixId: 0,
                levelId: 0
            };
            const options: MaintenanceMessageCommandOptions = {
                maintenanceFunction: MaintenanceFunction.HARD_RESET
            };

            // Act
            const command = new MaintenanceMessageCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params and options are out of range < MIN', done => {
            // Arrange
            const params: MaintenanceMessageCommandParams = {
                matrixId: -1,
                levelId: -1
            };
            const options: MaintenanceMessageCommandOptions = {
                maintenanceFunction: -1
            };

            // Act
            const fct = () => new MaintenanceMessageCommand(params, options);

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
                done();
            }
        });

        it('Should throw an error if params and options are out of range > MAX', done => {
            // Arrange
            const params: MaintenanceMessageCommandParams = {
                matrixId: 20,
                levelId: 16
            };
            const options: MaintenanceMessageCommandOptions = {
                maintenanceFunction: 255
            };

            // Act
            const fct = () => new MaintenanceMessageCommand(params, options);

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
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Maintenance Message CMD_007_0X07', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.MAINTENANCE_MESSAGE;
            });

            it('Should create & pack the general command (maintenanceFunction = MaintenanceFunction.HARD_RESET)', () => {
                // Arrange
                const params: MaintenanceMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0
                };
                const options: MaintenanceMessageCommandOptions = {
                    maintenanceFunction: MaintenanceFunction.HARD_RESET
                };

                // Act
                const command = new MaintenanceMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '07 00', // data
                    2, // bytesCount
                    0xf7, // checksum
                    '10 02 07 00 02 F7 10 03' // buffer
                );
            });
            it('Should create & pack the general command (maintenanceFunction = MaintenanceFunction.SOFT_RESET)', () => {
                // Arrange
                const params: MaintenanceMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0
                };
                const options: MaintenanceMessageCommandOptions = {
                    maintenanceFunction: MaintenanceFunction.SOFT_RESET
                };

                // Act
                const command = new MaintenanceMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '07 01', // data
                    2, // bytesCount
                    0xf6, // checksum
                    '10 02 07 01 02 F6 10 03' // buffer
                );
            });
            it('Should create & pack the general command (maintenanceFunction = MaintenanceFunction.DATABASE_TRANSFERT)', () => {
                // Arrange
                const params: MaintenanceMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0
                };
                const options: MaintenanceMessageCommandOptions = {
                    maintenanceFunction: MaintenanceFunction.DATABASE_TRANSFERT
                };

                // Act
                const command = new MaintenanceMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '07 04', // data
                    2, // bytesCount
                    0xf3, // checksum
                    '10 02 07 04 02 F3 10 03' // buffer
                );
            });
        });

        describe('to Log Description of the Clear Protects extended command', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.MAINTENANCE_MESSAGE;
            });

            it('Should create & pack the CLEAR PROTECTS command (maintenanceFunction = MaintenanceFunction.CLEAR_PROTECTS - matrixId = 0 - levelId = 0)', () => {
                // Arrange
                const params: MaintenanceMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0
                };
                const options: MaintenanceMessageCommandOptions = {
                    maintenanceFunction: MaintenanceFunction.CLEAR_PROTECTS
                };

                // Act
                const command = new MaintenanceMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '07 02 00 00', // data
                    4, // bytesCount
                    0xf3, // checksum
                    '10 02 07 02 00 00 04 F3 10 03' // buffer
                );
            });
            it('Should create & pack the CLEAR PROTECTS command (maintenanceFunction = MaintenanceFunction.CLEAR_PROTECTS - matrixId = 16 - levelId = 15)', () => {
                // Arrange
                const params: MaintenanceMessageCommandParams = {
                    matrixId: 16,
                    levelId: 15
                };
                const options: MaintenanceMessageCommandOptions = {
                    maintenanceFunction: MaintenanceFunction.CLEAR_PROTECTS
                };

                // Act
                const command = new MaintenanceMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '07 02 10 0F', // data
                    4, // bytesCount
                    0xd4, // checksum
                    '10 02 07 02 10 10 0F 04 D4 10 03' // buffer
                );
            });
            it('Should create & pack the CLEAR PROTECTS command (maintenanceFunction = MaintenanceFunction.CLEAR_PROTECTS - matrixId = 255 - levelId = 255)', () => {
                // Arrange
                const params: MaintenanceMessageCommandParams = {
                    matrixId: 255,
                    levelId: 255
                };
                const options: MaintenanceMessageCommandOptions = {
                    maintenanceFunction: MaintenanceFunction.CLEAR_PROTECTS
                };

                // Act
                const command = new MaintenanceMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '07 02 FF FF', // data
                    4, // bytesCount
                    0xf5, // checksum
                    '10 02 07 02 FF FF 04 F5 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log general command description', () => {
            // Arrange
            const params: MaintenanceMessageCommandParams = {
                matrixId: 0,
                levelId: 0
            };
            const options: MaintenanceMessageCommandOptions = {
                maintenanceFunction: MaintenanceFunction.SOFT_RESET
            };

            // Act
            const command = new MaintenanceMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log Clear Protects command description', () => {
            // Arrange
            const params: MaintenanceMessageCommandParams = {
                matrixId: 0xff,
                levelId: 0xff
            };
            const options: MaintenanceMessageCommandOptions = {
                maintenanceFunction: MaintenanceFunction.CLEAR_PROTECTS
            };

            // Act
            const command = new MaintenanceMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('clear protects')).toBe(true);
        });
    });
});
