import { CrossPointTieLineIneterrogateMessageCommand } from '../../../../src/command/rx/112-crosspoint-tie-line-interrogate-message/command';
import { CrossPointTieLineInterrogateMessageCommandParams } from '../../../../src/command/rx/112-crosspoint-tie-line-interrogate-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Tie Line Ineterrogate Message command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: CrossPointTieLineInterrogateMessageCommandParams = {
                matrixId: 0,
                destinationId: 0
            };

            // Act
            const command = new CrossPointTieLineIneterrogateMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: CrossPointTieLineInterrogateMessageCommandParams = {
                matrixId: -1,
                destinationId: -1
            };


            // Act
            const fct = () => new CrossPointTieLineIneterrogateMessageCommand(params);

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
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: CrossPointTieLineInterrogateMessageCommandParams = {
                matrixId: 256,
                destinationId: 65536
            };

            // Act
            const fct = () => new CrossPointTieLineIneterrogateMessageCommand(params);

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
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General CrossPoint Tie-Line Ineterrogate Message CMD_112_0X70', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_TIE_LINE_INTERROGATE_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: CrossPointTieLineInterrogateMessageCommandParams = {
                    matrixId: 0,
                    destinationId: 0
                };

                // Act
                const command = new CrossPointTieLineIneterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '70 00 00 00', // data
                    4, // bytesCount
                    0x8c, // checksum
                    '10 02 70 00 00 00 04 8C 10 03' // buffer
                );
            });


            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: CrossPointTieLineInterrogateMessageCommandParams = {
                    matrixId: 19,
                    destinationId: 65535
                };

                // Act
                const command = new CrossPointTieLineIneterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '70 13 ff ff', // data
                    4, // bytesCount
                    0x7b, // checksum
                    '10 02 70 13 ff ff 04 7b 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier =
                    CommandIdentifiers.RX.GENERAL.CROSSPOINT_TIE_LINE_INTERROGATE_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: CrossPointTieLineInterrogateMessageCommandParams = {
                    matrixId: 16,
                    destinationId: 16
                };

                // Act
                const command = new CrossPointTieLineIneterrogateMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '70 10 00 10', // data
                    4, // bytesCount
                    0x6c, // checksum
                    '10 02 70 10 10 00 10 10 04 6c 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log general command description', () => {
            // Arrange
            const params: CrossPointTieLineInterrogateMessageCommandParams = {
                matrixId: 19,
                destinationId: 65535
            };

            // Act
            const command = new CrossPointTieLineIneterrogateMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
