import { SingleSourceAssociationNamesRequestMessageCommand } from '../../../../src/command/rx/115-single-source-association-names-request-message/command';
import { SingleSourceAssociationNamesRequestMessageCommandParams } from '../../../../src/command/rx/115-single-source-association-names-request-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { SingleSourceAssociationNamesRequestMessageCommandOptions } from '../../../../src/command/rx/115-single-source-association-names-request-message/options';
import { LengthOfNamesRequired } from '../../../../src/command/shared/length-of-names-required';

describe('Single Source Association Names Request Message command', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                matrixId: 0,
                sourceId: 65535
            };
            const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new SingleSourceAssociationNamesRequestMessageCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                matrixId: -1,
                sourceId: -1
            };

            const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.SIXTEEN_CHAR_NAMES
            };
            // Act
            const fct = () => new SingleSourceAssociationNamesRequestMessageCommand(params, options);

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
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                matrixId: 16,
                sourceId: 65536
            };

            const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.SIXTEEN_CHAR_NAMES
            };
            // Act
            const fct = () => new SingleSourceAssociationNamesRequestMessageCommand(params, options);

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
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Single Source Association Names Request Message CMD_115_0X73', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.SINGLE_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    sourceId: 0
                };

                const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '73 00 00 00 00', // data
                    5, // bytesCount
                    0x88, // checksum
                    '10 02 73 00 00 00 00 05 88 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    sourceId: 0
                };

                const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '73 00 01 00 00', // data
                    5, // bytesCount
                    0x87, // checksum
                    '10 02 73 00 01 00 00 05 87 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    sourceId: 0
                };

                const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '73 00 02 00 00', // data
                    5, // bytesCount
                    0x86, // checksum
                    '10 02 73 00 02 00 00 05 86 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    sourceId: 896
                };

                const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '73 00 01 03 80', // data
                    5, // bytesCount
                    0x04, // checksum
                    '10 02 73 00 01 03 80 05 04 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    sourceId: 65534
                };

                const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '73 00 00 FF FE', // data
                    5, // bytesCount
                    0x8b, // checksum
                    '10 02 73 00 00 FF FE 05 8b 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    sourceId: 65535
                };

                const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '73 00 00 FF FF', // data
                    5, // bytesCount
                    0x8a, // checksum
                    '10 02 73 00 00 FF FF 05 8a 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier =
                    CommandIdentifiers.RX.GENERAL.SINGLE_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - sourceId=16))', () => {
                // Arrange
                const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    sourceId: 16
                };

                const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '73 00 02 00 10', // data
                    5, // bytesCount
                    0x76, // checksum
                    '10 02 73 00 02 00 10 10 05 76 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general command', () => {
        it('Should log general command description', () => {
            // Arrange
            const params: SingleSourceAssociationNamesRequestMessageCommandParams = {
                matrixId: 0,
                sourceId: 65535
            };

            const options: SingleSourceAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new SingleSourceAssociationNamesRequestMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
