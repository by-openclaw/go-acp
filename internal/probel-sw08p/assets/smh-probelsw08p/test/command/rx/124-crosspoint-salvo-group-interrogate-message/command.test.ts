import { CrossPointSalvoGroupInterrogateMessageCommand } from '../../../../src/command/rx/124-crosspoint-salvo-group-interrogate-message/command';
import { CrossPointSalvoGroupInterrogateMessageCommandParams } from '../../../../src/command/rx/124-crosspoint-salvo-group-interrogate-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Salvo Group Interrogate Message Command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                salvoId: 0,
                connectIndexId: 0
            };

            // Act
            const command = new CrossPointSalvoGroupInterrogateMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                salvoId: -1,
                connectIndexId: -1
            };

            // Act
            const fct = () => new CrossPointSalvoGroupInterrogateMessageCommand(params);

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
                expect(localeDataError.validationErrors?.connectIndexId.id).toBe(
                    CommandErrorsKeys.CONNECT_INDEX_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                salvoId: 128,
                connectIndexId: 65536
            };

            // Act
            const fct = () => new CrossPointSalvoGroupInterrogateMessageCommand(params);

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
                expect(localeDataError.validationErrors?.connectIndexId.id).toBe(
                    CommandErrorsKeys.CONNECT_INDEX_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General CrossPoint Salvo Group Interrogate Message CMD_124_0X7c', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_SALVO_GROUP_INTERROGATE_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                    salvoId: 0,
                    connectIndexId: 0
                };

                // Act
                const command = new CrossPointSalvoGroupInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7C 00 00', // data
                    3, // bytesCount
                    0x81, // checksum
                    '10 02 7C 00 00 03 81 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                    salvoId: 0,
                    connectIndexId: 1
                };

                // Act
                const command = new CrossPointSalvoGroupInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7C 00 01', // data
                    3, // bytesCount
                    0x80, // checksum
                    '10 02 7C 00 01 03 80 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                    salvoId: 0,
                    connectIndexId: 127
                };

                // Act
                const command = new CrossPointSalvoGroupInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7C 00 7F', // data
                    3, // bytesCount
                    0x02, // checksum
                    '10 02 7C 00 7F 03 02 10 03' // buffer
                );
            });

        });
        describe('Extended CrossPoint Salvo Group Interrogate Message CMD_248_0Xf8', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.CROSSPOINT_SALVO_GROUP_INTERROGATE_MESSAGE;
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                    salvoId: 0,
                    connectIndexId: 256
                };

                // Act
                const command = new CrossPointSalvoGroupInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'FC 00 01 00', // data
                    4, // bytesCount
                    0xff, // checksum
                    '10 02 FC 00 01 00 04 FF 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                    salvoId: 0,
                    connectIndexId: 65535
                };

                // Act
                const command = new CrossPointSalvoGroupInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'FC 00 FF FF', // data
                    4, // bytesCount
                    0x02, // checksum
                    '10 02 FC 00 FF FF 04 02 10 03' // buffer
                );
            });
        });
        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_SALVO_GROUP_INTERROGATE_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (salvoId=16 - levelId=16 - sourceId=16))', () => {
                // Arrange
                const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                    salvoId: 16,
                    connectIndexId: 16
                };

                // Act
                const command = new CrossPointSalvoGroupInterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7C 10 10', // data
                    3, // bytesCount
                    0x61, // checksum
                    '10 02 7C 10 10 10 10 03 61 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general command', () => {
        it('Should log general command description', () => {
            // Arrange
            const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                salvoId: 0,
                connectIndexId: 127
            };

            // Act
            const command = new CrossPointSalvoGroupInterrogateMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
        it('Should log extended command description', () => {
            // Arrange
            const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                salvoId: 127,
                connectIndexId: 65535
            };

            // Act
            const command = new CrossPointSalvoGroupInterrogateMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
