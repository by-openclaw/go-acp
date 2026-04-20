import { CrossPointGoGroupSalvoMessageCommand } from '../../../../src/command/rx/121-crosspoint-go-group-salvo-message/command';
import { CrossPointGoGroupSalvoMessageCommandParams } from '../../../../src/command/rx/121-crosspoint-go-group-salvo-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import {
    CrossPointGoGroupSalvoMessageCommandOptions,
    SalvoMessageFunction
} from '../../../../src/command/rx/121-crosspoint-go-group-salvo-message/options';

describe('Crosspoint Go Group Salvo Message command)', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: CrossPointGoGroupSalvoMessageCommandParams = {
                salvoId: 0
            };
            const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                salvoMessageFunction: SalvoMessageFunction.SET_PREVIOUS_RECEIVED_MESSAGES
            };
            // Act
            const command = new CrossPointGoGroupSalvoMessageCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: CrossPointGoGroupSalvoMessageCommandParams = {
                salvoId: -1
            };

            const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                salvoMessageFunction: -1
            };
            // Act
            const fct = () => new CrossPointGoGroupSalvoMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.salvoId.id).toBe(
                    CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: CrossPointGoGroupSalvoMessageCommandParams = {
                salvoId: 128
            };

            const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                salvoMessageFunction: 256
            };
            // Act
            const fct = () => new CrossPointGoGroupSalvoMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.salvoId.id).toBe(
                    CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Crosspoint Go Group Salvo Message CMD_121_0X79', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_GO_GROUP_SALVO_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: CrossPointGoGroupSalvoMessageCommandParams = {
                    salvoId: 0
                };

                const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                    salvoMessageFunction: SalvoMessageFunction.SET_PREVIOUS_RECEIVED_MESSAGES
                };
                // Act
                const command = new CrossPointGoGroupSalvoMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '79 00 00', // data
                    3, // bytesCount
                    0x84, // checksum
                    '10 02 79 00 00 03 84 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: CrossPointGoGroupSalvoMessageCommandParams = {
                    salvoId: 0
                };

                const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                    salvoMessageFunction: SalvoMessageFunction.CLEAR_PREVIOUSLY_RECEIVED_MESSAGES
                };
                // Act
                const command = new CrossPointGoGroupSalvoMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '79 01 00', // data
                    3, // bytesCount
                    0x83, // checksum
                    '10 02 79 01 00 03 83 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: CrossPointGoGroupSalvoMessageCommandParams = {
                    salvoId: 127
                };

                const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                    salvoMessageFunction: SalvoMessageFunction.SET_PREVIOUS_RECEIVED_MESSAGES
                };
                // Act
                const command = new CrossPointGoGroupSalvoMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '79 00 7F', // data
                    3, // bytesCount
                    0x05, // checksum
                    '10 02 79 00 7F 03 05 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: CrossPointGoGroupSalvoMessageCommandParams = {
                    salvoId: 127
                };

                const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                    salvoMessageFunction: SalvoMessageFunction.CLEAR_PREVIOUSLY_RECEIVED_MESSAGES
                };
                // Act
                const command = new CrossPointGoGroupSalvoMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '79 01 7F', // data
                    3, // bytesCount
                    0x04, // checksum
                    '10 02 79 01 7F 03 04 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_GO_GROUP_SALVO_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (salvoId=16 - levelId=16 - sourceId=16))', () => {
                // Arrange
                const params: CrossPointGoGroupSalvoMessageCommandParams = {
                    salvoId: 16
                };

                const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                    salvoMessageFunction: SalvoMessageFunction.SET_PREVIOUS_RECEIVED_MESSAGES
                };
                // Act
                const command = new CrossPointGoGroupSalvoMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '79 00 10', // data
                    3, // bytesCount
                    0x74, // checksum
                    '10 02 79 00 10 10 03 74 10 03' // buffer
                );
            });

            it('Should verify if [DLE] is duplicated (salvoId=16 - levelId=16 - sourceId=16))', () => {
                // Arrange
                const params: CrossPointGoGroupSalvoMessageCommandParams = {
                    salvoId: 16
                };

                const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                    salvoMessageFunction: SalvoMessageFunction.CLEAR_PREVIOUSLY_RECEIVED_MESSAGES
                };
                // Act
                const command = new CrossPointGoGroupSalvoMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '79 01 10', // data
                    3, // bytesCount
                    0x73, // checksum
                    '10 02 79 01 10 10 03 73 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general command', () => {
        it('Should log General command description - SET_PREVIOUS_RECEIVED_MESSAGES', () => {
            // Arrange
            const params: CrossPointGoGroupSalvoMessageCommandParams = {
                salvoId: 0
            };

            const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                salvoMessageFunction: SalvoMessageFunction.SET_PREVIOUS_RECEIVED_MESSAGES
            };
            // Act
            const command = new CrossPointGoGroupSalvoMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
        it('Should log General command description - CLEAR_PREVIOUSLY_RECEIVED_MESSAGES', () => {
            // Arrange
            const params: CrossPointGoGroupSalvoMessageCommandParams = {
                salvoId: 127
            };

            const options: CrossPointGoGroupSalvoMessageCommandOptions = {
                salvoMessageFunction: SalvoMessageFunction.CLEAR_PREVIOUSLY_RECEIVED_MESSAGES
            };
            // Act
            const command = new CrossPointGoGroupSalvoMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
