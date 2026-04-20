import { CrossPointTallyDumpRequestMessageCommand } from '../../../../src/command/rx/021-crosspoint-tally-dump-request-message/command';
import { CrossPointTallyDumpRequestMessageCommandParams } from '../../../../src/command/rx/021-crosspoint-tally-dump-request-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Tally Dump Request Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: CrossPointTallyDumpRequestMessageCommandParams = {
                matrixId: 0,
                levelId: 0
            };

            // Act
            const command = new CrossPointTallyDumpRequestMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: CrossPointTallyDumpRequestMessageCommandParams = {
                matrixId: -1,
                levelId: -1
            };

            // Act
            const fct = () => new CrossPointTallyDumpRequestMessageCommand(params /*, options*/);

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

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: CrossPointTallyDumpRequestMessageCommandParams = {
                matrixId: 256,
                levelId: 256
            };

            // Act
            const fct = () => new CrossPointTallyDumpRequestMessageCommand(params);

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
        describe('General CrossPoint Tally Dump Request Message CMD_021_0X15', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: CrossPointTallyDumpRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0
                };

                // Act
                const command = new CrossPointTallyDumpRequestMessageCommand(params /*, options*/);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '15 00', // data
                    2, // bytesCount
                    0xe9, // checksum
                    '10 02 15 00 02 E9 10 03' // buffer
                );
            });
        });

        describe('Extended General CrossPoint Tally Dump Request Message CMD_149_0X95', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE;
            });
            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: CrossPointTallyDumpRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0
                };

                // Act
                const command = new CrossPointTallyDumpRequestMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '95 10 00', // data
                    3, // bytesCount
                    0x58, // checksum
                    '10 02 95 10 10 00 03 58 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: CrossPointTallyDumpRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 16
                };

                // Act
                const command = new CrossPointTallyDumpRequestMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '95 00 10', // data
                    3, // bytesCount
                    0x58, // checksum
                    '10 02 95 00 10 10 03 58 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: CrossPointTallyDumpRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16
                };

                // Act
                const command = new CrossPointTallyDumpRequestMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '95 10 10', // data
                    3, // bytesCount
                    0x48, // checksum
                    '10 02 95 10 10 10 10 03 48 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: CrossPointTallyDumpRequestMessageCommandParams = {
                matrixId: 0,
                levelId: 0
            };

            // Act
            const command = new CrossPointTallyDumpRequestMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log Extended general command description', () => {
            // Arrange
            const params: CrossPointTallyDumpRequestMessageCommandParams = {
                matrixId: 16,
                levelId: 16
            };

            // Act
            const command = new CrossPointTallyDumpRequestMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
