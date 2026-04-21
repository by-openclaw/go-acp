import { CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand } from '../../../../src/command/tx/123-crosspoint-go-done-group-salvo-acknowledge-message/command';
import { CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams } from '../../../../src/command/tx/123-crosspoint-go-done-group-salvo-acknowledge-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import {
    CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions,
    SalvoCrossPointStatus
} from '../../../../src/command/tx/123-crosspoint-go-done-group-salvo-acknowledge-message/options';

describe('CrossPoint Go Done Group Salvo Acknowledge Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                salvoId: 0
            };

            const options: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions = {
                salvoCrossPointStatus: SalvoCrossPointStatus.CROSSPOINT_SET
            };
            // Act
            const command = new CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                salvoId: -1
            };

            const options: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions = {
                salvoCrossPointStatus: SalvoCrossPointStatus.CROSSPOINT_SET
            };

            // Act
            const fct = () => new CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand(params, options);

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
            const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                salvoId: 128
            };

            const options: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions = {
                salvoCrossPointStatus: SalvoCrossPointStatus.CROSSPOINT_SET
            };

            // Act
            const fct = () => new CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand(params, options);

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
        describe('General CrossPoint Go Done Group Salvo Acknowledge Message CMD_123_0X7b', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.CROSSPOINT_GO_DONE_GROUP_SALVO_ACKNOWLEDGE_MESSAGE;
            });

            it('Should create & pack the general command (salvoId = 0 - salvoCrossPointStatus = CROSSPOINT_SET)', () => {
                // Arrange
                const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                    salvoId: 0
                };

                const options: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions = {
                    salvoCrossPointStatus: SalvoCrossPointStatus.CROSSPOINT_SET
                };
                // Act
                const command = new CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7b 00 00', // data
                    3, // bytesCount
                    0x82, // checksum
                    '10 02 7b 00 00 03 82 10 03' // buffer
                );
            });
            it('Should create & pack the general command (salvoId = 15 - salvoCrossPointStatus = CROSSPOINT_TO_SET_OR_CLEAR)', () => {
                // Arrange
                const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                    salvoId: 0
                };

                const options: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions = {
                    salvoCrossPointStatus: SalvoCrossPointStatus.CROSSPOINT_TO_SET_OR_CLEAR
                };
                // Act
                const command = new CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7b 02 00', // data
                    3, // bytesCount
                    0x80, // checksum
                    '10 02 7b 02 00 03 80 10 03' // buffer
                );
            });
            it('Should create & pack the general command (salvoId = 0 - salvoCrossPointStatus = STORED_CROSSPOINTS_CLEARED)', () => {
                // Arrange
                const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                    salvoId: 0
                };

                const options: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions = {
                    salvoCrossPointStatus: SalvoCrossPointStatus.STORED_CROSSPOINTS_CLEARED
                };
                // Act
                const command = new CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7b 01 00', // data
                    3, // bytesCount
                    0x81, // checksum
                    '10 02 7b 01 00 03 81 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.CROSSPOINT_GO_DONE_GROUP_SALVO_ACKNOWLEDGE_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (salvoId=16))', () => {
                // Arrange
                const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                    salvoId: 16
                };

                const options: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions = {
                    salvoCrossPointStatus: SalvoCrossPointStatus.CROSSPOINT_SET
                };
                // Act
                const command = new CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7b 00 10', // data
                    3, // bytesCount
                    0x72, // checksum
                    '10 02 7b 00 10 10 03 72 10 03' // buffer
                );
            });
        });
    });
    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams = {
                salvoId: 0
            };

            const options: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions = {
                salvoCrossPointStatus: SalvoCrossPointStatus.CROSSPOINT_SET
            };
            // Act
            const command = new CrossPointGoDoneGroupSalvoAcknowledgeMessageCommand(params, options);

            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
