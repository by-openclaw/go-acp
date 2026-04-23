import { ProtectTallyDumpRequestMessageCommand } from '../../../../src/command/rx/019-protect-tally-dump-request-message/command';
import { ProtectTallyDumpRequestMessageCommandParams } from '../../../../src/command/rx/019-protect-tally-dump-request-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('Protect Tally Dump Request Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: ProtectTallyDumpRequestMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0
            };

            // Act
            const command = new ProtectTallyDumpRequestMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: ProtectTallyDumpRequestMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1
            };

            // Act
            const fct = () => new ProtectTallyDumpRequestMessageCommand(params);

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
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: ProtectTallyDumpRequestMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536
            };

            // Act
            const fct = () => new ProtectTallyDumpRequestMessageCommand(params);

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
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Protect Tally Dump Request Message CMD_019_0X13', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.PROTECT_TALLY_DUMP_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 0)', () => {
                // Arrange
                const params: ProtectTallyDumpRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0
                };

                // Act
                const command = new ProtectTallyDumpRequestMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '13 00 00 00', // data
                    4, // bytesCount
                    0xe9, // checksum
                    '10 02 13 00 00 00 04 E9 10 03' // buffer
                );
            });
        });

        describe('Extended General Protect Tally Dump Request Message CMD_147_0X93', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.PROTECT_TALLY_DUMP_REQUEST_MESSAGE;
            });
            it('Should create & pack the extended command (matrixId = 16 - levelId = 0 - destinationId = 895) matrixId > 15 & matrixId = [DLE]', () => {
                // Arrange
                const params: ProtectTallyDumpRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    destinationId: 895
                };

                // Act
                const command = new ProtectTallyDumpRequestMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '93 10 00 03 7F', // data
                    5, // bytesCount
                    0xd6, // checksum
                    '10 02 93 10 10 00 03 7F 05 D6 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 16 - destinationId = 895) levelId > 15 & matrixId = [DLE]', () => {
                // Arrange
                const params: ProtectTallyDumpRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 16,
                    destinationId: 895
                };

                // Act
                const command = new ProtectTallyDumpRequestMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '93 00 10 03 7F', // data
                    5, // bytesCount
                    0xd6, // checksum
                    '10 02 93 00 10 10 03 7F 05 D6 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 896) destinationId > 895', () => {
                // Arrange
                const params: ProtectTallyDumpRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896
                };

                // Act
                const command = new ProtectTallyDumpRequestMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '93 00 00 03 80', // data
                    5, // bytesCount
                    0xe5, // checksum
                    '10 02 93 00 00 03 80 05 E5 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.PROTECT_TALLY_DUMP_REQUEST_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: ProtectTallyDumpRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16
                };

                // Act
                const command = new ProtectTallyDumpRequestMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '93 10 10 00 10', // data
                    5, // bytesCount
                    0x38, // checksum
                    '10 02 93 10 10 10 10 00 10 10 05 38 10 03' // buffer
                );
            });
        });
    });
    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: ProtectTallyDumpRequestMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0
            };

            // Act
            const command = new ProtectTallyDumpRequestMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
        it('Should log Extended general command description', () => {
            // Arrange
            const params: ProtectTallyDumpRequestMessageCommandParams = {
                matrixId: 16,
                levelId: 16,
                destinationId: 896
            };

            // Act
            const command = new ProtectTallyDumpRequestMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
